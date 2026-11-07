// VPNGate CSV parsing adapts fanout (MIT); see NOTICE.md.
package main

import (
	"bufio"
	"cmp"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

type Node struct {
	Host    string `json:"host"`
	IP      string `json:"ip"`
	Country string `json:"country"`
	Port    int    `json:"port"`
	Network string `json:"network"`
	Speed   int64  `json:"speed"`
	Ping    int    `json:"ping"`
	Profile string `json:"profile"`
}

const (
	maxNodeSource = 16 << 20
	maxNodes      = 1500
)

var columnReplacer = strings.NewReplacer("_", "", "-", "", " ", "", "\ufeff", "")

func canonicalColumn(s string) string {
	return strings.ToLower(columnReplacer.Replace(strings.TrimPrefix(s, "#")))
}

func parseNodes(source io.Reader) ([]Node, error) {
	input := &io.LimitedReader{R: source, N: maxNodeSource + 1}
	nodes, err := parseNodeRecords(input)
	if input.N == 0 {
		return nil, errors.New("VPNGate response exceeds 16 MiB")
	}
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func parseNodeRecords(source io.Reader) ([]Node, error) {
	input := bufio.NewReader(source)
	var r *csv.Reader
	for {
		// Preamble and header lines fit in the 4 KiB reader buffer. ReadSlice
		// rejects an unbounded line without growing an intermediate string.
		line, err := input.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return nil, errors.New("VPNGate CSV preamble or header exceeds 4 KiB")
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		lc := strings.ToLower(string(line))
		if strings.Contains(lc, "openvpn_configdata_base64") && strings.Contains(lc, "country") {
			r = csv.NewReader(&boundedCSVReader{source: io.MultiReader(strings.NewReader(strings.TrimPrefix(string(line), "\ufeff")), input)})
			break
		}
		if err != nil {
			return nil, errors.New("VPNGate CSV header missing")
		}
	}
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	r.ReuseRecord = true
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	columns := map[string]int{}
	for i, k := range head {
		columns[canonicalColumn(k)] = i
	}
	column := func(keys ...string) int {
		for _, key := range keys {
			if i, ok := columns[key]; ok {
				return i
			}
		}
		return -1
	}
	ipColumn, countryColumn := column("ip"), column("countryshort", "countrycode")
	hostColumn, profileColumn := column("hostname", "ddnshostname"), column("openvpnconfigdatabase64")
	speedColumn, pingColumn := column("speed"), column("ping")
	var out []Node
	seen := map[string]bool{}
	var decoded []byte
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// Validate the full CSV even after the candidate cap; a broken tail must
		// not replace a valid cache. Extra records need no profile decoding.
		if len(out) == maxNodes {
			continue
		}
		get := func(i int) string {
			if i >= 0 && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		ip, cc := get(ipColumn), strings.ToUpper(get(countryColumn))
		if !publicIPv4(ip) || !countryPattern.MatchString(cc) {
			continue
		}
		host := get(hostColumn)
		if host == "" {
			host = strings.ReplaceAll(ip, ".", "-")
		}
		if !hostPattern.MatchString(host) || seen[host] {
			continue
		}
		encoded := get(profileColumn)
		if len(encoded) > base64.StdEncoding.EncodedLen(maxProfile) {
			continue
		}
		// Decode storage belongs to this refresh; normalized profiles own their
		// strings and cannot retain or alias the next record's scratch buffer.
		decoded, err = base64.StdEncoding.AppendDecode(decoded[:0], []byte(encoded))
		if err != nil {
			continue
		}
		profile, port, proto, err := validateProfile(string(decoded), ip)
		if err != nil {
			continue
		}
		speed, _ := strconv.ParseInt(get(speedColumn), 10, 64)
		ping, _ := strconv.Atoi(get(pingColumn))
		// CSV fields share the full record string, including its large base64 profile.
		host = strings.Clone(host)
		out = append(out, Node{Host: host, IP: strings.Clone(ip), Country: strings.Clone(cc), Port: port, Network: proto, Speed: speed, Ping: ping, Profile: profile})
		seen[host] = true
	}
	if len(out) == 0 {
		return nil, errors.New("no valid VPNGate profiles")
	}
	slices.SortStableFunc(out, func(a, b Node) int {
		if order := cmp.Compare(b.Speed, a.Speed); order != 0 {
			return order
		}
		return cmp.Compare(a.Ping, b.Ping)
	})
	return out, nil
}
