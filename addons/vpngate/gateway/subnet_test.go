package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSubnetAvoidsHostRoutes(t *testing.T) {
	for _, route := range []string{"10.231.254.1", "10.231.254.2/32", "10.231.254.0/31"} {
		subnet, err := chooseSubnet([]byte(fmt.Sprintf(`[{"dst":"default"},{"dst":%q}]`, route)))
		if err != nil || subnet != "10.232.254.0/30" {
			t.Fatalf("subnet overlaps host route %s: %s %v", route, subnet, err)
		}
	}
	if _, err := chooseSubnet([]byte(`[{"dst":"10.231.254.1"},{"dst":"10.232.254.2"},{"dst":"172.29.253.1"},{"dst":"192.168.223.3"}]`)); err == nil {
		t.Fatal("all candidate subnets overlap an existing host route")
	}
}

func BenchmarkChooseSubnet(b *testing.B) {
	routes := make([]map[string]string, 1000)
	for i := range routes {
		routes[i] = map[string]string{"dst": fmt.Sprintf("10.%d.%d.0/24", i/256, i%256)}
	}
	data, err := json.Marshal(routes)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := chooseSubnet(data); err != nil {
			b.Fatal(err)
		}
	}
}
