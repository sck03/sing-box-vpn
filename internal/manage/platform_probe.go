package manage

import (
	"context"
	"encoding/json"
	"errors"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"time"
)

func (s *PlatformService) OutboundOptions(ctx context.Context) (OutboundOptions, error) {
	c, state, err := s.appliedConfiguration(ctx)
	if err != nil {
		return OutboundOptions{}, err
	}
	config, err := serverForApp(c.App)
	if err != nil {
		return OutboundOptions{}, err
	}
	items := []OutboundOption{}
	for _, key := range []string{"outbounds", "endpoints"} {
		for _, item := range objects(config[key]) {
			items = append(items, OutboundOption{ID: text(item["tag"]), Type: text(item["type"])})
		}
	}
	return OutboundOptions{Items: items, Revision: state.AppliedRevision, Pending: state.Revision != state.AppliedRevision}, nil
}

func (s *PlatformService) probeOutbound(ctx context.Context, task int64, id, endpoint string) error {
	if err := sub.HTTPURL(endpoint); err != nil {
		return err
	}
	state, err := s.Store.AppliedState(ctx)
	if err != nil {
		return err
	}
	c, err := decodePlatform(state.Config)
	if err != nil {
		return err
	}
	config, err := serverForApp(c.App)
	if err != nil {
		return err
	}
	found := false
	for _, v := range append(objects(config["outbounds"]), objects(config["endpoints"])...) {
		if text(v["tag"]) == id {
			found = true
		}
	}
	if !found {
		return errors.New("unknown ordinary outbound; VPNGate routes are protected")
	}
	delete(config, "services")
	delete(config, "experimental")
	config["log"] = Object{"level": "error"}
	config["route"] = Object{"final": id, "default_domain_resolver": object(config["route"])["default_domain_resolver"]}
	started := time.Now()
	probeErr := probeCoreConfig(ctx, s.Root, s.Core, endpoint, config)
	result := Object{"outbound": id, "success": probeErr == nil, "milliseconds": time.Since(started).Milliseconds()}
	result["outcome"] = "passed"
	if probeErr != nil {
		result["outcome"] = "failed"
		result["error"] = probeErr.Error()
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.Store.Result(ctx, task, raw)
}
