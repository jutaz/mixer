package router

import (
	"github.com/siddontang/mixer/config"
	"gopkg.in/yaml.v1"
	"testing"
)

func TestParseRule(t *testing.T) {
	var s = `
rules:
-
    db: mixer
    table: test1 
    key: id
    # node will be node1, node2, ... node10
    type: hash
    nodes: node(1-10)

-
    db: mixer
    table: test2 
    key: name
    nodes: node1,node2,node3
    
    type: range
    # node1 range (-inf, 10000)
    # node2 range [10000, 20000)
    # node3 range [20000, +inf)
    range: -10000-20000-

-   db: mixer
    table: 
    key:
    nodes: node1
    type: default
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(s), &cfg); err != nil {
		t.Fatal(err)
	}

	rt, err := NewRouter(&cfg)
	if err != nil {
		t.Fatal(err)
	}

	if r := rt.GetDBRules("mixer"); r == nil {
		t.Fatal("must not nil")
	}

	hashRule := rt.GetRule("mixer", "test1")
	if hashRule.Type != HashRuleType {
		t.Fatal(hashRule.Type)
	}

	if n := hashRule.FindNode(uint64(11)); n != "node2" {
		t.Fatal(n)
	}

	rangeRule := rt.GetRule("mixer", "test2")
	if rangeRule.Type != RangeRuleType {
		t.Fatal(rangeRule.Type)
	}

	if n := rangeRule.FindNode(10000); n != "node2" {
		t.Fatal(n)
	}

	defaultRule := rt.GetRule("mixer", "test3")
	if defaultRule == nil {
		t.Fatal("must not nil")
	}

	if defaultRule.Type != DefaultRuleType {
		t.Fatal(defaultRule.Type)
	}

	if n := defaultRule.FindNode(11); n != "node1" {
		t.Fatal(n)
	}
}
