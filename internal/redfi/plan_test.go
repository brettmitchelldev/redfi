package redfi

import (
	"fmt"
	"reflect"
	"testing"
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

	rule := p.SelectRule("192.0.0.1", []byte(""), MakeLogger(0))
	if rule == nil {
		t.Fatal("rule must not be nil")
	}

	// test command matching
	p.Rules = []*Rule{}
	p.Rules = append(p.Rules, &Rule{
		Delay:   1e3,
		Command: "GET",
	})
	p.MarshalCommands()

	rule = p.SelectRule("192.0.0.1", []byte("*1\r\n$3\r\nGET\r\nfff"), MakeLogger(0))
	if rule == nil {
		t.Fatal("rule must not be nil")
	}

	rule = p.SelectRule("172.0.0.1", []byte("\r\nKEYS\r\nfff"), MakeLogger(0))
	if rule != nil {
		fmt.Println(rule)
		t.Fatal("rule must BE nil")
	}

}

func TestSelectRuleRawMatchAll(t *testing.T) {
	cases := []struct {
		name     string
		plan     *Plan
		addr     string
		msg      []byte
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
			msg:  []byte("asdfasdfasdf abc asdfasdfasdf 123 djdkfjdkfjDFJ"),
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
			addr: "0.0.0.0",
			msg:  []byte("asdfasdfasdf 123 asdfasdfasdf"),
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
			addr: "0.0.0.0",
			msg:  []byte("asdfasdfasdf asdfasdfasdf"),
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
		msg      []byte
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
			msg:  []byte("asdfasdfasdf abc asdfasdfasdf"),
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
			msg:  []byte("asdfasdfasdf 123 asdfasdfasdf 321"),
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
			msg:      []byte("asdfasdfasdf xyz asdfasdfasdf"),
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
