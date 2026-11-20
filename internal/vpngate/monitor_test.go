package vpngate

import (
	"context"
	"net/http"
)

func (m *manager) monitor(ctx context.Context, c Config, active tunnel, n Node, client *http.Client) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go watchTunnel(ctx, active, cancel)
	return m.monitorSession(ctx, c, active, n, client)
}
