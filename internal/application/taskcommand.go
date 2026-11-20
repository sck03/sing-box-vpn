package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/servicecatalog"
)

// TaskCommand is the public HTTP/CLI command. Scheduling metadata never crosses
// this boundary. Each operation has an explicit set of meaningful fields.
type TaskCommand struct {
	Operation    string              `json:"operation"`
	Service      string              `json:"service,omitempty"`
	ID           string              `json:"id,omitempty"`
	Value        string              `json:"value,omitempty"`
	Config       json.RawMessage     `json:"config,omitempty"`
	Account      AccountCommand      `json:"account,omitzero"`
	Subscription SubscriptionCommand `json:"subscription,omitzero"`
}

// CommandFields also drives the generated frontend discriminated union.
var CommandFields = map[string][]string{
	"service":           {"service", "value"},
	"gateway":           {"config"},
	"account":           {"account"},
	"subscription":      {"subscription"},
	"certificate-renew": {"id"},
	"bbr":               {"value"},
	"program-upgrade":   {"value"},
	"runtime-rollback":  {},
	"validator-install": {},
	"outbound-probe":    {"id", "value"},
}

func (c TaskCommand) Validate() error {
	allowed, ok := CommandFields[c.Operation]
	if !ok {
		return errors.New("unknown task operation")
	}
	v, t := reflect.ValueOf(c), reflect.TypeOf(c)
	for i := 1; i < v.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if !v.Field(i).IsZero() && !slices.Contains(allowed, name) {
			return fmt.Errorf("field %s is not allowed for %s", name, c.Operation)
		}
	}
	switch c.Operation {
	case "service":
		if !slices.Contains(servicecatalog.Names(), c.Service) || !slices.Contains([]string{"start", "stop", "restart", "enable", "disable"}, c.Value) {
			return errors.New("unsupported service action")
		}
	case "gateway":
		_, err := c.GatewayOperation()
		return err
	case "account":
		if !slices.Contains([]string{"bind", "unbind", "rotate"}, c.Account.Operation) {
			return errors.New("account task must bind, unbind or rotate credentials")
		}
		if c.Account.Operation != "bind" && (len(c.Account.AccountIDs) != 0 || len(c.Account.InboundIDs) != 0) {
			return errors.New("account selection is only valid when binding")
		}
		if c.Account.Operation == "unbind" && (c.Account.UUID != "" || c.Account.Password != "") {
			return errors.New("unbind cannot contain replacement credentials")
		}
		if c.Account.Operation == "bind" && c.Account.Generation != 0 {
			return errors.New("binding cannot select a credential generation")
		}
		_, err := NormalizeCredentialInput(c.Account)
		return err
	case "subscription":
		if !slices.Contains([]string{"refresh", "check", "deliver"}, c.Subscription.Operation) {
			return errors.New("unsupported subscription task")
		}
		if c.Subscription.Operation != "check" && c.Subscription.ProbeURL != "" {
			return errors.New("probe_url is only valid for subscription checks")
		}
	case "certificate-renew", "outbound-probe":
		if c.ID == "" {
			return errors.New("task requires an id")
		}
	case "bbr":
		if !slices.Contains([]string{"enable", "restore"}, c.Value) {
			return errors.New("unsupported network action")
		}
	}
	return nil
}

func (c TaskCommand) GatewayOperation() (string, error) {
	var v struct {
		Op string `json:"op"`
	}
	d := json.NewDecoder(bytes.NewReader(c.Config))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return "", errors.New("invalid gateway command")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return "", errors.New("expected one gateway command")
	}
	if !slices.Contains([]string{"start", "stop", "swap", "refresh"}, v.Op) {
		return "", errors.New("unsupported gateway action")
	}
	return v.Op, nil
}

func (c TaskCommand) Background() bool {
	return c.Operation == "outbound-probe" || c.Operation == "subscription" && c.Subscription.Operation == "check"
}
