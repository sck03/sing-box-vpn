package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func BenchmarkNodeSource(b *testing.B) {
	var data strings.Builder
	writer := csv.NewWriter(&data)
	writer.Write([]string{"HostName", "IP", "CountryShort", "OpenVPN_ConfigData_Base64"})
	profile := strings.Replace(safeTestProfile, "test-certificate", strings.Repeat("A", 8192), 1)
	encoded := base64.StdEncoding.EncodeToString([]byte(profile))
	for i := 0; i < 250; i++ {
		writer.Write([]string{fmt.Sprintf("node-%d", i), "203.0.113.5", "JP", encoded})
	}
	writer.Flush()
	body := data.String()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testHTTPResponse(body), nil
	})}
	for _, pipeline := range []string{"Buffered", "Streaming"} {
		b.Run(pipeline, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				var nodes []Node
				var err error
				if pipeline == "Buffered" {
					var data []byte
					data, err = readHTTP(context.Background(), client, "https://nodes.example", maxNodeSource)
					if err == nil {
						nodes, err = parseNodes(bytes.NewReader(data))
					}
				} else {
					nodes, err = readNodeSource(context.Background(), client, "https://nodes.example")
				}
				if err != nil || len(nodes) != 250 {
					b.Fatalf("source returned %d nodes, %v", len(nodes), err)
				}
			}
		})
	}
}
