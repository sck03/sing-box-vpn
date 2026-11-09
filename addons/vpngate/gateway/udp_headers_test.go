package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestUDPHeaderBounds(t *testing.T) {
	ip, err := udpResponseHeader("203.0.113.1:53")
	if err != nil {
		t.Fatal(err)
	}
	domain := append([]byte{0, 0, 0, 3, 12}, []byte("example.test")...)
	domain = binary.BigEndian.AppendUint16(domain, 53)
	for _, header := range [][]byte{ip, domain} {
		if size, err := udpHeaderSize(header); err != nil || size != len(header) {
			t.Fatal("valid UDP header rejected", size, err)
		}
		zeroPort := bytes.Clone(header)
		zeroPort[len(zeroPort)-2], zeroPort[len(zeroPort)-1] = 0, 0
		if _, err := udpHeaderSize(zeroPort); err == nil {
			t.Fatal("zero target port accepted", zeroPort)
		}
	}
	for _, header := range [][]byte{nil, ip[:9], domain[:len(domain)-1], {0, 0, 0, 3, 0, 0, 53}, {0, 0, 0, 4}} {
		if _, err := udpHeaderSize(header); err == nil {
			t.Fatal("malformed or unsupported UDP header accepted", header)
		}
	}
}

func TestUDPAddressFormsShareCanonicalSession(t *testing.T) {
	proxy, _, _ := startProxyFixture(t)
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		var first string
		var data [16]byte
		for {
			_, peer, err := server.ReadFromUDPAddrPort(data[:])
			if err != nil {
				return
			}
			if first == "" {
				first = peer.String()
			}
			reply := byte(0)
			if first == peer.String() {
				reply = 1
			}
			_, _ = server.WriteToUDPAddrPort([]byte{reply}, peer)
		}
	}()
	control := authProxy(t, proxy, "password")
	defer control.Close()
	_, _ = control.Write(requestAddress(3, "0.0.0.0:0"))
	var bind [10]byte
	if _, err := io.ReadFull(control, bind[:]); err != nil || bind[1] != 0 {
		t.Fatal("UDP association failed", bind, err)
	}
	client, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(bind[4:8]), Port: int(binary.BigEndian.Uint16(bind[8:]))})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	ipHeader, err := udpResponseHeader(server.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1"
	domainHeader := append([]byte{0, 0, 0, 3, byte(len(host))}, host...)
	domainHeader = binary.BigEndian.AppendUint16(domainHeader, uint16(server.LocalAddr().(*net.UDPAddr).Port))
	var answer [128]byte
	for _, header := range [][]byte{ipHeader, domainHeader, ipHeader, domainHeader} {
		if _, err := client.Write(append(bytes.Clone(header), 'x')); err != nil {
			t.Fatal(err)
		}
		n, err := client.Read(answer[:])
		if err != nil || n != 11 || answer[10] != 1 {
			t.Fatal("alternate address form changed the UDP source session", n, err)
		}
	}
}
