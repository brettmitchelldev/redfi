package redfi

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/tidwall/redcon"
	pool "gopkg.in/fatih/pool.v2"
)

type Proxy struct {
	server   string
	plan     *Plan
	addr     string
	apiAddr  string
	connPool pool.Pool
	api      *API
	logging  string
}

func (p *Proxy) Plan() *Plan {
	return p.plan
}

func factory(server string) func() (net.Conn, error) {
	return func() (net.Conn, error) {
		return net.Dial("tcp", server)
	}
}

func New(planPath, server, addr, apiAddr, logging string) (*Proxy, error) {
	p, err := pool.NewChannelPool(5, 30, factory(server))
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
		server:   server,
		connPool: p,
		plan:     plan,
		addr:     addr,
		api:      NewAPI(plan),
		apiAddr:  apiAddr,
		logging:  logging,
	}, nil
}

func (p *Proxy) StartAPI() {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	// r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// RESTy routes for "rules" resource
	r.Route("/rules", func(r chi.Router) {
		r.Get("/", p.api.listRules) // GET /rules

		r.Post("/", p.api.createRule) // POST /rules

		// Subrouters:
		r.Route("/{ruleName}", func(r chi.Router) {
			r.Get("/", p.api.getRule)       // GET /rules/drop_20
			r.Delete("/", p.api.deleteRule) // DELETE /rules/drop_20
		})
	})

	fmt.Printf("control\t%s\n", p.apiAddr)
	err := http.ListenAndServe(p.apiAddr, r)
	if err != nil {
		log.Fatal(err)
	}
}

func (p *Proxy) Start(logger Logger) error {
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("redis\t%s\n", p.server)
	fmt.Printf("proxy\t%s\n", p.addr)

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

	targetConn, err := p.connPool.Get()
	if err != nil {
		log.Fatal("failed to get a connection from connPool")
	}

	wg.Add(2)
	go func() {
		p.faulter(targetConn, conn, logger)
		wg.Done()
	}()
	go func() {
		p.pipe(conn, targetConn)
		wg.Done()
	}()
	wg.Wait()

	log.Println("Close connection", conn.Close())
}

func (p *Proxy) pipe(dst, src net.Conn) {
	buf := make([]byte, 32<<10)

	for {
		n, err := src.Read(buf)
		if err != nil && err == io.EOF {
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

func (p *Proxy) faulter(dst, src net.Conn, logger Logger) {
	srcRd := bufio.NewReader(src)

	for {
		var buf []byte
    var msg redcon.RESP
		// Read a complete RESP command and preserve any extra data (will be part of the next packet)
		for {
			line, err := srcRd.ReadBytes('\n')
			if err != nil {
				log.Println(err)
				if err == io.EOF {
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

		rule := p.plan.SelectRule(src.RemoteAddr().String(), msg, logger)

		if rule != nil {
			if rule.Delay > 0 {
        logger(1, fmt.Sprintf("Delaying packet: rule = %s, delay = %dms\n", rule.Name, rule.Delay))
				time.Sleep(time.Duration(rule.Delay) * time.Millisecond)
        logger(1, fmt.Sprintf("Delay complete, sending message: rule = %s\n", rule.Name))
			}

			if rule.Drop {
        logger(1, fmt.Sprintf("Dropping connection with client: rule = %s", rule.Name))
				err := src.Close()
				if err != nil {
					log.Println("encountered error while closing srcConn", err)
				}
				break
			}

			if rule.ReturnEmpty {
        logger(1, fmt.Sprintf("Returning empty: rule = %s", rule.Name))
				_, err := dst.Write([]byte("$-1\r\n"))
				if err != nil {
					log.Println(err)
				}
				continue
			}

			if len(rule.ReturnErr) > 0 {
        logger(1, fmt.Sprintf("Returning error: rule = %s, error = '%s'", rule.Name, rule.ReturnErr))
				buf := []byte{}
				buf = redcon.AppendError(buf, rule.ReturnErr)
				_, err := dst.Write(buf)
				if err != nil {
					log.Println(err)
				}
				continue
			}
		}

		_, err := dst.Write(msg.Raw)
		if err != nil {
			log.Println(err)
			continue
		}

	}
}
