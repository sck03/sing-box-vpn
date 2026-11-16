package subscription

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCollectionRejectsDuplicateAndOversizedReferences(t *testing.T) {
	for _, count := range []int{2, MaxObjects + 1} {
		c := DefaultConfig()
		c.Collections[0].Sources = make([]string, count)
		for i := range c.Collections[0].Sources {
			c.Collections[0].Sources[i] = "normal"
		}
		if err := c.Validate(); err == nil {
			t.Fatal("invalid expansion accepted")
		}
	}
}

func TestCombinedIntermediateBudget(t *testing.T) {
	c := DefaultConfig()
	c.Collections = nil
	c.Outputs = nil
	for i := 0; i < 5; i++ {
		c.Collections = append(c.Collections, Collection{ID: fmt.Sprint("set", i), Sources: []string{"normal"}, Processor: Processor{Deduplicate: true}})
	}
	nodes := make([]Node, MaxNodes)
	for i := range nodes {
		nodes[i] = Node{Name: "node", Protocol: "vmess", Server: "example.com", Port: 443, UUID: "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"}
	}
	_, _, err := Build(c, Runtime{}, map[string][]Node{"normal": nodes}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatal("combined intermediate budget not enforced", err)
	}
}
