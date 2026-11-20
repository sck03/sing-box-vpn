package vpngate

import (
	"github.com/sck03/sing-box-vpn/internal/gatewayconfig"
	"github.com/sck03/sing-box-vpn/internal/statefile"
	"io"
	"os"
	"time"
)

type Config gatewayconfig.Config

func defaults() Config            { return Config(gatewayconfig.Default()) }
func (c *Config) validate() error { return (*gatewayconfig.Config)(c).Validate() }
func readConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, gatewayconfig.MaxConfigBytes+1))
	if err != nil {
		return Config{}, err
	}
	c, err := gatewayconfig.Decode(data)
	if err == nil {
		err = c.Validate()
	}
	return Config(c), err
}
func atomicJSON(path string, value any) error { return statefile.AtomicJSON(path, value) }

type Status struct {
	Version string    `json:"version"`
	State   string    `json:"state"`
	Country string    `json:"country"`
	Mode    string    `json:"mode"`
	Node    string    `json:"node,omitempty"`
	Exit    *Geo      `json:"exit,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	Updated time.Time `json:"updated"`
}
