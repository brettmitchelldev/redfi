package redfi

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"

	// "net/http"
	"os"
	"sync"
	"time"

	// "github.com/go-chi/chi"
	// "github.com/go-chi/chi/middleware"

	"github.com/tidwall/redcon"
	pool "gopkg.in/fatih/pool.v2"
)

type Proxy struct {
	redisAddr string
	plan			*Plan
	addr			string
	apiAddr	 string
	connPool	pool.Pool
	// api			 *API
	logging string
}

func (p *Proxy) Plan() *Plan {
	return p.plan
}

func factory(server string) func() (net.Conn, error) {
	return func() (net.Conn, error) {
		return net.Dial("tcp", server)
	}
}

func New(planPath, redisAddr, addr, apiAddr, logging string) (*Proxy, error) {
	p, err := pool.NewChannelPool(5, 30, factory(redisAddr))
	if err != nil {
		return nil, err
	}

	plan := NewPlan()
	if len(planPath) > 0 {
		// parse the failures plan
		plan, err = Parse(planPath)
		if err != nil {
			return nil, err
		}
	}

	return &Proxy{
		redisAddr: redisAddr,
		connPool:	p,
		plan:			plan,
		addr:			addr,
		// api:			 NewAPI(plan),
		apiAddr: apiAddr,
		logging: logging,
	}, nil
}

// func (p *Proxy) StartAPI() {
// 	r := chi.NewRouter()
// 	r.Use(middleware.RequestID)
// 	r.Use(middleware.RealIP)
// 	// r.Use(middleware.Recoverer)
// 	r.Use(middleware.Timeout(60 * time.Second))
//
// 	// RESTy routes for "rules" resource
// 	r.Route("/rules", func(r chi.Router) {
// 		r.Get("/", p.api.listRules) // GET /rules
//
// 		r.Post("/", p.api.createRule) // POST /rules
//
// 		// Subrouters:
// 		r.Route("/{ruleName}", func(r chi.Router) {
// 			r.Get("/", p.api.getRule)			 // GET /rules/drop_20
// 			r.Delete("/", p.api.deleteRule) // DELETE /rules/drop_20
// 		})
// 	})
//
// 	fmt.Printf("control %s\n", p.apiAddr)
// 	err := http.ListenAndServe(p.apiAddr, r)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// }

func (p *Proxy) Start(logger Logger) error {
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("redis	 %s\n", p.redisAddr)
	fmt.Printf("proxy	 %s\n", p.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go p.handle(conn, logger)
	}
}

func (p *Proxy) handle(conn net.Conn, logger Logger) {
	var wg sync.WaitGroup

	// targetConn, err := p.connPool.Get()
	targetConn, err := net.Dial("tcp", p.redisAddr)
	if err != nil {
		log.Fatal("failed to get a connection from connPool")
	}

	wg.Add(2)
	go func() {
		p.requestFaulter(targetConn, conn, logger)
		wg.Done()
	}()
	go func() {
		p.responseFaulter(conn, targetConn, logger)
		wg.Done()
	}()
	wg.Wait()

	log.Println("Close connection", conn.Close())
}

func (p *Proxy) pipe(dst, src net.Conn) {
	buf := make([]byte, 32<<10)

	for {
		n, err := src.Read(buf)
		if err != nil && (err == io.EOF || err == os.ErrClosed) {
			break
		}
		if err != nil {
			log.Println(err)
			continue
		}

		// @TODO(kl): check if written is less than what's in buf
		_, err = dst.Write(buf[:n])
		if err != nil {
			log.Println(err)
			continue
		}
	}
}

func (p *Plan) handleRule(streamType string, msg redcon.RESP, rule *Rule, src, dst net.Conn, logger Logger) {
	if rule != nil && rule.Delay > 0 {
		logger(1, fmt.Sprintf("%s :: Delaying packet: rule = %s, delay = %dms\n", streamType, rule.Name, rule.Delay))
		time.Sleep(time.Duration(rule.Delay) * time.Millisecond)
		logger(1, fmt.Sprintf("%s :: Delay complete, sending message: rule = %s\n", streamType, rule.Name))
	}

	p.m.Lock()
	defer p.m.Unlock()

	if rule != nil {
		if rule.Drop {
			logger(1, fmt.Sprintf("%s :: Dropping connection with client: rule = %s", streamType, rule.Name))
			err := src.Close()
			if err != nil {
				log.Println("encountered error while closing srcConn", err)
			}
			return
		}

		if rule.ReturnEmpty {
			logger(1, fmt.Sprintf("%s :: Returning empty: rule = %s", streamType, rule.Name))
			_, err := dst.Write([]byte("$-1\r\n"))
			if err != nil {
				log.Println(err)
			}
		}

		if len(rule.ReturnErr) > 0 {
			logger(1, fmt.Sprintf("%s :: Returning error: rule = %s, error = '%s'", streamType, rule.Name, rule.ReturnErr))
			buf := []byte{}
			buf = redcon.AppendError(buf, rule.ReturnErr)
			_, err := dst.Write(buf)
			if err != nil {
				log.Println(err)
			}
		}
	}

	_, err := dst.Write(msg.Raw)
	if err != nil {
		log.Println(err)
	}
}

func (p *Proxy) requestFaulter(dst, src net.Conn, logger Logger) {
	srcRd := bufio.NewReader(src)

	for {
		var buf []byte
		var msg redcon.RESP
		// Read a complete RESP command and preserve any extra data (will be part of the next packet)
		for {
			line, err := srcRd.ReadBytes('\n')
			if err != nil {
				log.Println(err)
				if err == io.EOF || err == os.ErrClosed {
					return
				}
			}

			buf = append(buf, line...)
			n, resp := redcon.ReadNextRESP(buf)
			if n != 0 {
				msg = resp
				break
			}
		}

		clientAddr := src.RemoteAddr().String()
		p.plan.handleClientSetName(clientAddr, msg)
		rule := p.plan.SelectRule("REQUEST", p.plan.RequestRules, clientAddr, msg, logger)

		if p.plan.MsgOrdering == "unordered" || (rule != nil && p.plan.MsgOrdering == "unordered-delays" && rule.Delay > 0) {
			go p.plan.handleRule("REQUEST", msg, rule, src, dst, logger)
		} else {
			p.plan.handleRule("REQUEST", msg, rule, src, dst, logger)
		}
	}
}

func (p *Proxy) responseFaulter(dst, src net.Conn, logger Logger) {
	srcRd := bufio.NewReader(src)

	for {
		var buf []byte
		var msg redcon.RESP
		// Read a complete RESP command and preserve any extra data (will be part of the next packet)
		for {
			line, err := srcRd.ReadBytes('\n')
			if err != nil {
				log.Println(err)
				if err == io.EOF || err == os.ErrClosed {
					return
				}
			}

			buf = append(buf, line...)
			n, resp := redcon.ReadNextRESP(buf)
			if n != 0 {
				msg = resp
				break
			}
		}

		clientAddr := dst.RemoteAddr().String()
		p.plan.handleClientSetName(clientAddr, msg)
		rule := p.plan.SelectRule("RESPONSE", p.plan.ResponseRules, clientAddr, msg, logger)

		if p.plan.MsgOrdering == "unordered" || (rule != nil && p.plan.MsgOrdering == "unordered-delays" && rule.Delay > 0) {
			go p.plan.handleRule("RESPONSE", msg, rule, src, dst, logger)
		} else {
			p.plan.handleRule("RESPONSE", msg, rule, src, dst, logger)
		}
	}
}
