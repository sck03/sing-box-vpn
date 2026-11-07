package manage

import "testing"

var benchmarkClient Object

func BenchmarkClientConfig(b *testing.B) {
	nodes := make([]Object, len(Protocols))
	for i, kind := range Protocols {
		nodes[i] = Object{"type": kind, "tag": kind}
	}
	for _, test := range []struct {
		name    string
		gateway bool
	}{{"normal", false}, {"vpngate", true}} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkClient = ClientConfig(nodes, test.gateway)
			}
		})
	}
}
