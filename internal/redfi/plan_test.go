package redfi

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/tidwall/redcon"
)

func TestSelectRule(t *testing.T) {
	p := &Plan{
		Rules: []*Rule{},
	}

	// // test ip matching
	p.Rules = append(p.Rules, &Rule{
		Delay:      1e3,
		ClientAddr: "192.0.0.1:8001",
	})

	_, resp := redcon.ReadNextRESP([]byte(""))
	rule := p.SelectRule("192.0.0.1", resp, MakeLogger(0))
	if rule == nil {
		t.Fatal("rule must not be nil")
	}

	// test command matching
	p.Rules = []*Rule{}
	p.Rules = append(p.Rules, &Rule{
		Delay:   1e3,
		Command: "GET",
	})

	_, resp = redcon.ReadNextRESP([]byte("*1\r\n$3\r\nGET\r\nfff"))
	rule = p.SelectRule("192.0.0.1", resp, MakeLogger(0))
	if rule == nil {
		t.Fatal("rule must not be nil")
	}

	_, resp = redcon.ReadNextRESP([]byte("\r\nKEYS\r\nfff"))
	rule = p.SelectRule("172.0.0.1", resp, MakeLogger(0))
	if rule != nil {
		fmt.Println(rule)
		t.Fatal("rule must BE nil")
	}
}

func Resp(b []byte) redcon.RESP {
	_, resp := redcon.ReadNextRESP(b)
	return resp
}

func TestSelectRuleRawMatchAll(t *testing.T) {
	cases := []struct {
		name     string
		plan     *Plan
		addr     string
		msg      redcon.RESP
		expected *Rule
	}{
		{
			name: "msg contains all options of second rule",
			plan: &Plan{
				Rules: []*Rule{
					{
						Name:        "1",
						RawMatchAll: []string{"321", "123"},
					},
					{
						Name:        "2",
						RawMatchAll: []string{"123", "abc"},
					},
				},
			},
			addr: "0.0.0.0",
			msg:  Resp([]byte("*4\r\n$3\r\nddd\r\n$3\r\nabc\r\n$3\r\nddd\r\n$3\r\n123\r\n")),
			expected: &Rule{
				Name:        "2",
				RawMatchAll: []string{"123", "abc"},
				hits:        1,
			},
		},

		{
			name: "msg contains only one option of second rule",
			plan: &Plan{
				Rules: []*Rule{
					{
						Name:        "1",
						RawMatchAll: []string{"321", "123"},
					},
					{
						Name:        "2",
						RawMatchAll: []string{"123", "abc"},
					},
				},
			},
			addr:     "0.0.0.0",
			msg:      Resp([]byte("*4\r\n$3\r\nddd\r\n$3\r\nabc\r\n$3\r\nddd\r\n$3\r\naaa\r\n")),
			expected: nil,
		},

		{
			name: "msg contains no matches",
			plan: &Plan{
				Rules: []*Rule{
					{
						Name:        "1",
						RawMatchAll: []string{"321", "123"},
					},
					{
						Name:        "2",
						RawMatchAll: []string{"123", "abc"},
					},
				},
			},
			addr:     "0.0.0.0",
			msg:      Resp([]byte("*4\r\n$3\r\nddd\r\n$3\r\naaa\r\n$3\r\nddd\r\n$3\r\na23\r\n")),
			expected: nil,
		},
	}

	for _, c := range cases {
		output := c.plan.SelectRule(c.addr, c.msg, MakeLogger(0))
		if !reflect.DeepEqual(c.expected, output) {
			t.Fatal(fmt.Sprintf(
				"Case failed:\n\t%s:\n\texpected = %#v\n\toutput   = %#v",
				c.name,
				c.expected,
				output,
			))
		}
	}
}

func TestSelectRuleRawMatchAny(t *testing.T) {
	cases := []struct {
		name     string
		plan     *Plan
		addr     string
		msg      redcon.RESP
		expected *Rule
	}{
		{
			name: "msg contains second option of second rule",
			plan: &Plan{
				Rules: []*Rule{
					{
						Name:        "1",
						RawMatchAny: []string{"321", "123"},
					},
					{
						Name:        "2",
						RawMatchAny: []string{"123", "abc"},
					},
				},
			},
			addr: "0.0.0.0",
			msg:  Resp([]byte("*4\r\n$3\r\nddd\r\n$3\r\nabc\r\n$3\r\nddd\r\n$3\r\naaa\r\n")),
			expected: &Rule{
				Name:        "2",
				RawMatchAny: []string{"123", "abc"},
				hits:        1,
			},
		},

		{
			name: "msg contains more than one match",
			plan: &Plan{
				Rules: []*Rule{
					{
						Name:        "1",
						RawMatchAny: []string{"321", "123"},
					},
					{
						Name:        "2",
						RawMatchAny: []string{"abc", "123"},
					},
				},
			},
			addr: "0.0.0.0",
			msg:  Resp([]byte("*4\r\n$3\r\nddd\r\n$3\r\n321\r\n$3\r\nddd\r\n$3\r\n123\r\n")),
			expected: &Rule{
				Name:        "1",
				RawMatchAny: []string{"321", "123"},
				hits:        1,
			},
		},

		{
			name: "msg contains no matches",
			plan: &Plan{
				Rules: []*Rule{
					{
						Name:        "1",
						RawMatchAny: []string{"321", "123"},
					},
					{
						Name:        "2",
						RawMatchAny: []string{"abc", "123"},
					},
				},
			},
			addr:     "0.0.0.0",
			msg:      Resp([]byte("*4\r\n$3\r\naaa\r\n$3\r\nbbb\r\n$3\r\nccc\r\n$3\r\nddd\r\n")),
			expected: (*Rule)(nil),
		},
	}

	for _, c := range cases {
		output := c.plan.SelectRule(c.addr, c.msg, MakeLogger(0))
		if !reflect.DeepEqual(c.expected, output) {
			t.Fatal(fmt.Sprintf(
				"Case failed:\n\t%s:\n\texpected = %#v\n\toutput   = %#v",
				c.name,
				c.expected,
				output,
			))
		}
	}
}

func TestAddDeleteGetRule(t *testing.T) {
	p := NewPlan()

	r := Rule{
		Name:       "clients_delay",
		Delay:      50,
		Percentage: 20,
	}
	p.AddRule(r)

	if len(p.Rules) != 1 {
		t.Fatal("rule wasn't added")
	}
	if !(p.Rules[0].Delay == r.Delay && p.Rules[0].Percentage == r.Percentage) {
		t.Fatal("rule added doesn't match")
	}

	fetchedRule, err := p.GetRule("clients_delay")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(fetchedRule)

	err = p.DeleteRule("clients_delay")
	if err != nil {
		t.Fatal(err)
	}

	_, err = p.GetRule("clients_delay")
	if err == nil {
		t.Fatal(err)
	}
	fmt.Println(fetchedRule)

}
