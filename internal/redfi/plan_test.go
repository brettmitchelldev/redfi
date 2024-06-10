package redfi

import (
	"fmt"
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
