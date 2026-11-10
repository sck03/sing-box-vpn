package manage

import (
	"errors"
	"io"
	"strings"
	"testing"
)

const inventoryHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"

func TestPortInventory(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		tcp        bool
		own        int
		wantError  bool
	}{
		{"tcp-listener", "0: 00000000:7918 00000000:0000 0A", true, 0, true},
		{"tcp-established", "0: 00000000:7918 00000000:01BB 01", true, 0, false},
		{"tcp-owned", "0: 00000000:7918 00000000:0000 0A", true, 31000, false},
		{"udp-connected", "0: 00000000:7918 00000000:0035 01", false, 0, true},
		{"udp-unconnected", "0: 00000000:7918 00000000:0000 07", false, 0, true},
		{"ipv6", "0: 00000000000000000000000000000000:7918 00000000000000000000000000000000:0000 0A", true, 0, true},
		{"outside-range", "0: 00000000:7917 00000000:0000 0A", true, 0, false},
		{"short-record", "0: 00000000:7918", true, 0, true},
		{"missing-port", "0: 00000000 00000000:0000 0A", false, 0, true},
		{"invalid-port", "0: 00000000:ZZZZ 00000000:0000 0A", true, 0, true},
		{"overflow-port", "0: 00000000:10000 00000000:0000 0A", true, 0, true},
		{"invalid-state", "0: 00000000:7918 00000000:0000 ZZ", true, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkPortInventory(strings.NewReader(inventoryHeader+tc.line+"\n"), tc.tcp, []portRange{{31000, 31001}}, tc.own)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tc.wantError)
			}
		})
	}
	if err := checkPortInventory(strings.NewReader(inventoryHeader), true, nil, 0); err != nil {
		t.Fatal("empty inventory rejected", err)
	}
}

type failedInventoryReader struct{ err error }

func (r failedInventoryReader) Read([]byte) (int, error) { return 0, r.err }

func TestPortInventoryReadFailure(t *testing.T) {
	want := errors.New("inventory unavailable")
	input := io.MultiReader(strings.NewReader(inventoryHeader), failedInventoryReader{want})
	if err := checkPortInventory(input, true, nil, 0); !errors.Is(err, want) {
		t.Fatal("read error lost", err)
	}
}

func BenchmarkPortInventory(b *testing.B) {
	data := inventoryHeader + strings.Repeat("   0: 00000000:7918 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 12345 1 0000000000000000 100 0 0 10 0\n", 4096)
	ranges := []portRange{{32000, 32001}}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if err := checkPortInventory(strings.NewReader(data), true, ranges, 0); err != nil {
			b.Fatal(err)
		}
	}
}
