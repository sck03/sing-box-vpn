package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type packetWriter struct {
	bytes.Buffer
	calls int
}

func (w *packetWriter) Write(data []byte) (int, error) {
	w.calls++
	return w.Buffer.Write(data)
}

type shortPacketWriter struct{}

func (shortPacketWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestPacketWritePreservesFrameBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 1400, maxPacket} {
		payload := bytes.Repeat([]byte{0x5a}, size)
		packet := append(make([]byte, 2, size+2), payload...)
		var w packetWriter
		if err := writePacket(&w, packet); err != nil {
			t.Fatal(err)
		}
		got, err := readFrame(&w.Buffer, maxPacket)
		if err != nil || !bytes.Equal(got, payload) || w.calls != 1 {
			t.Fatalf("packet %d: writes=%d, error=%v", size, w.calls, err)
		}
	}
}

func TestPacketWriteRejectsShortWritesAndInvalidSizes(t *testing.T) {
	if err := writePacket(shortPacketWriter{}, make([]byte, 3)); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short packet write was accepted", err)
	}
	for _, size := range []int{0, 1, maxPacket + 3} {
		if err := writePacket(io.Discard, make([]byte, size)); err == nil {
			t.Fatal("invalid packet size was accepted", size)
		}
	}
}

func BenchmarkUDPFrameWrite(b *testing.B) {
	packet := make([]byte, 1402)
	b.ReportAllocs()
	b.SetBytes(1400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writePacket(io.Discard, packet); err != nil {
			b.Fatal(err)
		}
	}
}
