package manage

import (
	"encoding/json"
	"errors"

	"github.com/sck03/sing-box-vpn/internal/serverconfig"
)

const AppSchema = 7

type ManagedNode serverconfig.Node
type ManagedTLS = serverconfig.TLS
type ManagedTransport = serverconfig.Transport

func (n ManagedNode) inbound() Object { return serverconfig.Node(n).Inbound() }
func (n ManagedNode) Validate() error { return serverconfig.Node(n).Validate() }

func managedNodes(app App) ([]ManagedNode, error) {
	if len(app.Nodes) > 256 {
		return nil, errors.New("at most 256 inbounds")
	}
	return app.Nodes, nil
}

// Inbounds returns a detached core projection, never the authoritative state.
func (a App) Inbounds() []Object {
	inbounds := make([]Object, 0, len(a.Nodes))
	for _, n := range a.Nodes {
		inbounds = append(inbounds, n.inbound())
	}
	return inbounds
}

func (a App) MarshalJSON() ([]byte, error) {
	type document App
	return json.Marshal(struct {
		Schema int `json:"schema"`
		document
	}{AppSchema, document(a)})
}
func (a *App) UnmarshalJSON(data []byte) error {
	type document App
	var d struct {
		Schema int `json:"schema"`
		document
	}
	if err := strictSubscriptionJSON(data, &d); err != nil {
		return err
	}
	if d.Schema != AppSchema {
		return errors.New("unsupported app schema; use a fresh configuration directory")
	}
	*a = App(d.document)
	return nil
}
