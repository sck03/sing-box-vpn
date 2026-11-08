package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const maxASN = 1<<32 - 1

// Parse the original JSON number, without float64 rounding before policy checks.
type geoASNNumber uint32

func (n *geoASNNumber) UnmarshalJSON(data []byte) error {
	value, err := strconv.ParseUint(string(data), 10, 32)
	if err != nil || value == 0 {
		return errors.New("geolocation ASN must be a positive 32-bit integer")
	}
	*n = geoASNNumber(value)
	return nil
}

type geoASN struct {
	Number geoASNNumber `json:"asn"`
	Type   string       `json:"type"`
	Org    string       `json:"org"`
	ISP    string       `json:"isp"`
}

// Decode only fields used for exit verification. Providers can add metadata
// without making us allocate maps and slices for their entire response.
type geoResponse struct {
	Success     *bool           `json:"success"`
	Error       json.RawMessage `json:"error"`
	IP          json.RawMessage `json:"ip"`
	Query       json.RawMessage `json:"query"`
	CountryCode string          `json:"country_code"`
	CountryAlt  string          `json:"countryCode"`
	Country     string          `json:"country"`
	Location    struct {
		Country string `json:"country_code"`
	} `json:"location"`
	Datacenter *bool   `json:"is_datacenter"`
	Hosting    *bool   `json:"hosting"`
	ASN        *geoASN `json:"asn"`
	Connection *geoASN `json:"connection"`
	ISP        string  `json:"isp"`
}

func parseGeo(data []byte, ip string) (Geo, error) {
	var response geoResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return Geo{}, err
	}
	if response.Success != nil && !*response.Success {
		return Geo{}, errors.New("geolocation lookup failed")
	}
	switch string(response.Error) {
	case "", "null", "false", `""`:
	default:
		return Geo{}, errors.New("geolocation provider reported an error")
	}
	reported := response.IP
	if reported == nil {
		reported = response.Query
	}
	if reported != nil {
		var address string
		if err := json.Unmarshal(reported, &address); err != nil {
			return Geo{}, errors.New("geolocation address does not match the tunnel exit")
		}
		if parsed := net.ParseIP(address); parsed == nil || !parsed.Equal(net.ParseIP(ip)) {
			return Geo{}, errors.New("geolocation address does not match the tunnel exit")
		}
	}
	g := Geo{IP: ip, Country: strings.ToUpper(cmp.Or(response.CountryCode, response.CountryAlt, response.Location.Country, response.Country))}
	if !countryPattern.MatchString(g.Country) {
		return g, errors.New("geolocation country missing")
	}
	for _, hosting := range []*bool{response.Datacenter, response.Hosting} {
		if hosting != nil {
			g.HostingKnown = true
			g.Hosting = g.Hosting || *hosting
		}
	}
	if asn := cmp.Or(response.ASN, response.Connection); asn != nil {
		g.ASN = int(asn.Number)
		g.ASNType = strings.ToLower(asn.Type)
		g.ISP = cmp.Or(asn.Org, asn.ISP)
	}
	switch g.ASNType {
	case "hosting", "datacenter":
		g.Hosting, g.HostingKnown = true, true
	case "isp":
		g.HostingKnown = true
	}
	g.ISP = cmp.Or(g.ISP, response.ISP)
	if g.ISP == "" {
		g.ISP = fmt.Sprintf("AS%d", g.ASN)
	}
	return g, nil
}
