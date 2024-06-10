package redfi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"github.com/tidwall/redcon"
)

var (
	// ErrNotFound is returned iff SelectRule can't find a Rule that applies
	ErrNotFound = errors.New("no matching rule found")
)

// Plan defines a set of rules to be applied by the proxy
type Plan struct {
	MsgOrdering   string  `json:"msgOrdering,omitempty"`
	RequestRules  []*Rule `json:"requestRules,omitempty"`
	ResponseRules []*Rule `json:"responseRules,omitempty"`
	// a lookup table mapping rule name to index in the array
	rulesMap map[string]int

	// a lookup table mapping network addresses to known client names
	clientNameMap map[string]string

	m sync.RWMutex
}

// Rule is what get's applied on every client message iff it matches it
type Rule struct {
	Name        string `json:"name,omitempty"`
	Delay       int    `json:"delay,omitempty"`
	Drop        bool   `json:"drop,omitempty"`
	ReturnEmpty bool   `json:"returnEmpty,omitempty"`
	ReturnErr   string `json:"returnErr,omitempty"`
	Percentage  int    `json:"percentage,omitempty"`
	Log         bool   `json:"log,omitempty"`

	// SelectRule does prefix matching on this value
	ClientAddr  string   `json:"clientAddr,omitempty"`
	ClientName  string   `json:"clientName,omitempty"`
	Command     string   `json:"command,omitempty"`
	RawMatchAny []string `json:"rawMatchAny,omitempty"`
	RawMatchAll []string `json:"rawMatchAll,omitempty"`
	AlwaysMatch bool     `json:"alwaysMatch,omitempty"`

	hits uint64
}

func (r Rule) String() string {
	buf := []string{}
	buf = append(buf, r.Name)

	// count hits
	hits := atomic.LoadUint64(&r.hits)
	buf = append(buf, fmt.Sprintf("hits=%d", hits))

	if r.Delay > 0 {
		buf = append(buf, fmt.Sprintf("delay=%d", r.Delay))
	}
	if r.Drop {
		buf = append(buf, fmt.Sprintf("drop=%t", r.Drop))
	}
	if r.ReturnEmpty {
		buf = append(buf, fmt.Sprintf("returnEmpty=%t", r.ReturnEmpty))
	}
	if len(r.ReturnErr) > 0 {
		buf = append(buf, fmt.Sprintf("returnErr=%s", r.ReturnErr))
	}
	if len(r.ClientAddr) > 0 {
		buf = append(buf, fmt.Sprintf("clientAddr=%s", r.ClientAddr))
	}
	if r.Percentage > 0 {
		buf = append(buf, fmt.Sprintf("percentage=%d", r.Percentage))
	}

	return strings.Join(buf, " ")
}

// Parse the plan.json file
func Parse(planPath string) (*Plan, error) {
	fullPath, err := filepath.Abs(planPath)
	if err != nil {
		return nil, err
	}

	fd, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}

	buf, err := io.ReadAll(fd)
	if err != nil {
		return nil, err
	}

	// this is the plan we will use
	plan := &Plan{
		rulesMap:      map[string]int{},
		clientNameMap: map[string]string{},
	}

	// this is a draft of the plan
	// we use to parse the json file,
	// then copy its rules to the real plan
	err = json.Unmarshal(buf, plan)
	if err != nil {
		return nil, err
	}

	// for i, rule := range plan.RequestRules {
	// 	err := plan.AddRuleMeta(*rule)
	// 	if err != nil {
	// 		return plan, fmt.Errorf("encountered error when adding rule #%d: %s", i, err)
	// 	}
	// }

	return plan, nil
}

func NewPlan() *Plan {
	return &Plan{
		MsgOrdering:   "ordered",
		RequestRules:  []*Rule{},
		ResponseRules: []*Rule{},
		rulesMap:      map[string]int{},
	}
}

func respArrToSlice(resp redcon.RESP) ([]redcon.RESP, error) {
	if resp.Type != redcon.Array {
		return nil, fmt.Errorf("RESP packet is not an Array")
	}

	out := make([]redcon.RESP, 0)
	resp.ForEach(func(r redcon.RESP) bool {
		out = append(out, r)
		return true
	})
	return out, nil
}

func rlower(resp redcon.RESP) string {
	return strings.ToLower(string(resp.Data))
}

func (p *Plan) handleClientSetName(clientAddr string, resp redcon.RESP) {
	respSlice, err := respArrToSlice(resp)
	if err != nil {
		return
	}

	if len(respSlice) != 3 {
		return
	}

	if rlower(respSlice[0]) != "client" || rlower(respSlice[1]) != "setname" {
		return
	}

	p.m.Lock()
	p.clientNameMap[clientAddr] = string(respSlice[2].Data)
	p.m.Unlock()
}

func (p *Plan) pickRule(rules []*Rule, clientAddr string, msg redcon.RESP, log Logger) *Rule {
	for _, rule := range rules {
		log(3, fmt.Sprintf("Checking rule: rule = %s, client = %s\n", rule.Name, clientAddr))

		if rule.AlwaysMatch == true {
			return rule
		}

		hasClientName := len(rule.ClientName) > 0
		hasClientAddr := len(rule.ClientAddr) > 0
		hasCommand := len(rule.Command) > 0
		hasRawMatchAny := len(rule.RawMatchAny) > 0
		hasRawMatchAll := len(rule.RawMatchAll) > 0

		matches := (hasClientName || hasClientAddr || hasCommand || hasRawMatchAny || hasRawMatchAll)

		if hasClientName {
			p.m.RLock()
			clientName, ok := p.clientNameMap[clientAddr]
			p.m.RUnlock()
			matches = matches && ok && clientName == rule.ClientName
		}

		if hasClientAddr {
			matches = matches && !strings.HasPrefix(clientAddr, rule.ClientAddr)
		}

		if hasCommand {
			if msg.Type != redcon.Array {
				matches = false
				continue
			}
			msg.ForEach(func(r redcon.RESP) bool {
				matches = matches && string(r.Data) == rule.Command
				// Redis sends the command name as the first element in an array of bulk strings
				return false
			})
		}

		if hasRawMatchAny {
			hasAny := false
			for _, fragment := range rule.RawMatchAny {
				if bytes.Contains(msg.Data, []byte(fragment)) {
					hasAny = true
					break
				}
			}
			matches = matches && hasAny
		}

		if hasRawMatchAll {
			for _, fragment := range rule.RawMatchAll {
				matches = matches && bytes.Contains(msg.Data, []byte(fragment))
			}
		}

		if matches {
			return rule
		}
	}

	return nil
}

func clean(s string) string {
	return strings.Map(func(c rune) rune {
		if c == '\n' || c == '\r' {
			return c
		}
		if !unicode.IsPrint(c) && c > unicode.MaxASCII {
			return -1
		}
		return c
	}, s)
}

// SelectRule finds the first rule that applies to the given variables
func (p *Plan) SelectRule(streamType string, rules []*Rule, clientAddr string, msg redcon.RESP, log Logger) *Rule {
	rule := p.pickRule(rules, clientAddr, msg, log)

	if rule == nil {
		return nil
	}

  log(1, fmt.Sprintf("\n>>> %s :: Rule '%s' matched a command\n", streamType, rule.Name))
	if rule.Log == false {
		log(2, fmt.Sprintf("command = \"\n%s\n\"\n", clean(string(msg.Data))))
	}

	if rule.Log == true {
		asBytes, err := json.Marshal(rule)
		if err == nil {
			log(0, fmt.Sprintf("matched rule: %s\n", string(asBytes)))
		}
		p.m.RLock()
		clientName := p.clientNameMap[clientAddr]
		p.m.RUnlock()
		log(0, fmt.Sprintf("matched client: client addr = %s, client name = %s\n", clientAddr, clientName))
		log(0, fmt.Sprintf("matched command: %s\n", clean(string(msg.Data))))
	}

	if rule.Percentage > 0 && rand.Intn(100) > rule.Percentage {
		log(1, "skipped due to percentage setting\n")
		return nil
	}

	newHits := atomic.AddUint64(&rule.hits, 1)
	log(2, fmt.Sprintf("times applied = %d\n", newHits))
	return rule
}

// // AddRuleMeta adds a rule to the current working plan
// func (p *Plan) AddRuleMeta(kind string, r Rule) error {
//   if r.Percentage < 0 || r.Percentage > 100 {
//   	return fmt.Errorf("Percentage in rule #%s is malformed. it must within 0-100", r.Name)
//   }
//
//   if len(r.Name) <= 0 {
//   	return fmt.Errorf("Name of rule is required")
//   }
//
//   p.m.Lock()
//   defer p.m.Unlock()
//   if _, ok := p.rulesMap[kind+r.Name]; ok {
//   	return fmt.Errorf("a rule by the same name exists")
//   }
//   
//   p.rulesMap[kind+r.Name] = len(p.RequestRules) - 1
//
//   return nil
// }
//
// // TODO: Update HTTP API to distinguish between request and response rules
//
// // DeleteRule deletes the given ruleName if found
// // otherwise it returns ErrNotFound
// func (p *Plan) DeleteRule(name string) error {
// 	p.m.Lock()
// 	defer p.m.Unlock()
//
// 	idx, ok := p.rulesMap[name]
// 	if !ok {
// 		return ErrNotFound
// 	}
//
// 	p.RequestRules = append(p.RequestRules[:idx], p.RequestRules[idx+1:]...)
// 	delete(p.rulesMap, name)
//
// 	return nil
// }
//
// // GetRule returns the rule that matches the given name
// func (p *Plan) GetRule(name string) (Rule, error) {
// 	p.m.RLock()
// 	defer p.m.RUnlock()
//
// 	idx, ok := p.rulesMap[name]
// 	if !ok {
// 		return Rule{}, ErrNotFound
// 	}
//
// 	return *p.RequestRules[idx], nil
// }
//
// // ListRules returns a slice of all the existing rules
// // the slice will be empty if Plan has no rules
// func (p *Plan) ListRules() []Rule {
// 	p.m.RLock()
// 	defer p.m.RUnlock()
//
// 	rules := []Rule{}
// 	for _, rule := range p.RequestRules {
// 		rules = append(rules, *rule)
// 	}
//
// 	return rules
// }
