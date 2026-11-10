package manage

import (
	"io"
	"strings"
	"testing"
)

// Isolate encoding from disk and core process startup. Both paths emit the same
// validation JSON; buffering measures the extra copy used before streaming.
func BenchmarkValidationJSONEncoding(b *testing.B) {
	value := Object{"route": Object{"rules": []Object{{"domain_suffix": strings.Split(strings.Repeat("example.com,", 65536), ","), "outbound": "proxy"}}}}
	for _, buffered := range []bool{true, false} {
		name := "Streamed"
		if buffered {
			name = "Buffered"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if buffered {
					data, err := JSONBytes(value)
					if err != nil {
						b.Fatal(err)
					}
					_, _ = io.Discard.Write(data)
				} else if err := jsonEncoder(io.Discard).Encode(value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
