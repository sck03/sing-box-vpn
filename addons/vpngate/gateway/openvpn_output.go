package main

import (
	"bytes"
	"io"
)

const openVPNReady = "Initialization Sequence Completed"
const maxOpenVPNLog = 2 << 20

// os/exec serializes writes from the shared stdout/stderr pipe and joins its
// copier in Wait. Keep draining even after the log fills or its disk fails.
type openVPNOutput struct {
	log      io.Writer
	written  int
	ready    chan struct{}
	tail     [len(openVPNReady) - 1]byte
	tailSize int
}

func (o *openVPNOutput) Write(p []byte) (int, error) {
	if o.ready != nil {
		// A readiness marker may straddle any two pipe reads, even one byte reads.
		var boundary [2 * (len(openVPNReady) - 1)]byte
		n := copy(boundary[:], o.tail[:o.tailSize])
		n += copy(boundary[n:], p[:min(len(p), len(o.tail))])
		if bytes.Contains(boundary[:n], []byte(openVPNReady)) || bytes.Contains(p, []byte(openVPNReady)) {
			close(o.ready)
			o.ready = nil
		} else if len(p) >= len(o.tail) {
			o.tailSize = copy(o.tail[:], p[len(p)-len(o.tail):])
		} else {
			o.tailSize = copy(o.tail[:], boundary[max(0, n-len(o.tail)):n])
		}
	}
	if o.log != nil && o.written < maxOpenVPNLog {
		part := p[:min(len(p), maxOpenVPNLog-o.written)]
		n, err := o.log.Write(part)
		o.written += n
		if err != nil || n != len(part) {
			o.log = nil
		}
	}
	return len(p), nil
}
