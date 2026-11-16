// Package encoding provides bounded, strict JSON decoding for transport adapters.
package encoding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func JSON(data []byte, target any, limit int) error {
	if len(data) == 0 || len(data) > limit || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("empty or oversized JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON document")
	}
	return nil
}
