package main

import (
	"bytes"
	"errors"
	"io"
)

const (
	maxCSVColumns = 64
	maxCSVRecord  = 2 << 20 // Room for a 1 MiB base64 profile and metadata.
)

// Bound CSV structure before encoding/csv allocates its record and field slices.
// Quote parity locates separators in valid CSV, including doubled quotes and
// multiline fields; encoding/csv remains responsible for validating the grammar.
type boundedCSVReader struct {
	source  io.Reader
	quoted  bool
	commas  int
	bytes   int
	failure error
}

func (r *boundedCSVReader) Read(p []byte) (int, error) {
	if r.failure != nil {
		return 0, r.failure
	}
	n, err := r.source.Read(p)
	for offset := 0; offset < n; {
		rest := p[offset:n]
		end := len(rest)
		if i := bytes.IndexByte(rest, '"'); i >= 0 {
			end = i
		}
		if !r.quoted {
			if i := bytes.IndexByte(rest[:end], '\n'); i >= 0 {
				end = i
			}
			r.commas += bytes.Count(rest[:end], []byte{','})
			if r.commas >= maxCSVColumns {
				r.failure = errors.New("VPNGate CSV record exceeds 64 columns")
				return offset, r.failure
			}
		}
		size := min(end+1, len(rest))
		r.bytes += size
		if r.bytes > maxCSVRecord {
			r.failure = errors.New("VPNGate CSV record exceeds 2 MiB")
			return offset, r.failure
		}
		offset += size
		if end == len(rest) {
			break
		}
		switch rest[end] {
		case '"':
			r.quoted = !r.quoted
		case '\n':
			r.bytes, r.commas = 0, 0
		}
	}
	return n, err
}
