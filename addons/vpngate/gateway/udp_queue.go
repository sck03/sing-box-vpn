package main

import (
	"net"
	"sync"
)

type queuedUDPPacket struct {
	data  []byte
	class int
}

// Keep small queued datagrams small; a full-size receive buffer per queue entry
// would retain far more memory than the configured byte budget permits.
var udpQueuePools [9]sync.Pool

func init() {
	for i := range udpQueuePools {
		class, capacity := i, 256<<i
		udpQueuePools[i].New = func() any {
			return &queuedUDPPacket{data: make([]byte, capacity), class: class}
		}
	}
}

// queue, closeQueue and releasePacket require the association lock. Capacity,
// including a packet currently being written, counts against both byte budgets.
func (u *udpAssociation) queue(s *udpSession, data []byte) bool {
	if len(data) < 2 || len(data) > maxPacket+2 {
		return false
	}
	class, capacity := 0, 256
	for capacity < len(data) {
		class++
		capacity *= 2
	}
	if s.closed || len(s.queued) == cap(s.queued) || u.queuedBytes+capacity > maxUDPQueuedBytes || s.queuedBytes+capacity > maxUDPTargetBytes {
		return false
	}
	packet := udpQueuePools[class].Get().(*queuedUDPPacket)
	packet.data = packet.data[:len(data)]
	copy(packet.data, data)
	u.queuedBytes += capacity
	s.queuedBytes += capacity
	s.queued <- packet
	return true
}

func (u *udpAssociation) closeQueue(s *udpSession) {
	if !s.closed {
		s.closed = true
		close(s.queued)
	}
}

func (u *udpAssociation) releasePacket(s *udpSession, packet *queuedUDPPacket) {
	u.queuedBytes -= cap(packet.data)
	s.queuedBytes -= cap(packet.data)
	udpQueuePools[packet.class].Put(packet)
}

func (u *udpAssociation) send(s *udpSession, c net.Conn) {
	for {
		select {
		case <-u.ctx.Done():
			return
		case packet, ok := <-s.queued:
			if !ok {
				return
			}
			err := sendUDPPacket(c, packet.data)
			u.mu.Lock()
			u.releasePacket(s, packet)
			u.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
