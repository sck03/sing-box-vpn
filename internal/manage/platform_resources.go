package manage

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
)

// Validate the resolved graph, including generated defaults. The core remains
// responsible for each protocol's option schema and platform capabilities.
func validateEffectiveServer(server Object) error {
	graph := map[string][]string{}
	out := map[string]bool{}
	dns := map[string]bool{}
	inbound := map[string]bool{}
	sets := map[string]bool{}
	for _, v := range objects(server["inbounds"]) {
		inbound[text(v["tag"])] = true
	}
	all := append(append([]Object{}, objects(server["outbounds"])...), objects(server["endpoints"])...)
	for _, v := range all {
		out[text(v["tag"])] = true
	}
	d := object(server["dns"])
	for _, v := range objects(d["servers"]) {
		tag := text(v["tag"])
		if tag == "" || dns[tag] {
			return errors.New("DNS tags must be unique and nonempty")
		}
		dns[tag] = true
	}
	r := object(server["route"])
	resolverID := func(v any) string {
		if id := text(v); id != "" {
			return id
		}
		return text(object(v)["server"])
	}
	defaultResolver := resolverID(r["default_domain_resolver"])
	needsResolver := func(v Object) bool {
		host := text(v["server"])
		if host == "" {
			return false
		}
		_, e := netip.ParseAddr(host)
		return e != nil
	}
	for _, v := range objects(r["rule_set"]) {
		tag := text(v["tag"])
		if tag == "" || sets[tag] {
			return errors.New("rule-set tags must be unique and nonempty")
		}
		sets[tag] = true
	}
	ref := func(kind, id string) error {
		exists := false
		switch kind {
		case "out":
			exists = out[id]
		case "dns":
			exists = dns[id]
		case "inbound":
			exists = inbound[id]
		case "rule_set":
			exists = sets[id]
		}
		if id != "" && !exists {
			return fmt.Errorf("missing %s reference %s", kind, id)
		}
		return nil
	}
	for _, v := range all {
		tag := "out/" + text(v["tag"])
		graph[tag] = nil
		refs := append([]string{}, stringsValue(v["outbounds"])...)
		if id := text(v["detour"]); id != "" {
			refs = append(refs, id)
		}
		if id := text(v["default"]); id != "" && !slices.Contains(stringsValue(v["outbounds"]), id) {
			return errors.New("selector default must be a member")
		}
		for _, id := range refs {
			if err := ref("out", id); err != nil {
				return err
			}
			graph[tag] = append(graph[tag], "out/"+id)
		}
		resolver := text(v["domain_resolver"])
		if resolver == "" {
			resolver = text(object(v["domain_resolver"])["server"])
		}
		if resolver == "" && needsResolver(v) && text(v["detour"]) == "" {
			resolver = defaultResolver
		}
		if resolver != "" {
			if err := ref("dns", resolver); err != nil {
				return err
			}
			graph[tag] = append(graph[tag], "dns/"+resolver)
		}
	}
	for _, v := range objects(d["servers"]) {
		tag := "dns/" + text(v["tag"])
		graph[tag] = nil
		if id := text(v["detour"]); id != "" {
			if err := ref("out", id); err != nil {
				return err
			}
			graph[tag] = append(graph[tag], "out/"+id)
		}
		resolver := text(v["domain_resolver"])
		if resolver == "" {
			resolver = text(object(v["domain_resolver"])["server"])
		}
		if resolver != "" {
			if err := ref("dns", resolver); err != nil {
				return err
			}
			graph[tag] = append(graph[tag], "dns/"+resolver)
		}
	}
	if err := ref("out", text(r["final"])); err != nil {
		return err
	}
	if err := ref("dns", text(d["final"])); err != nil {
		return err
	}
	if err := ref("dns", defaultResolver); err != nil {
		return err
	}
	var rules func([]Object, bool) error
	rules = func(items []Object, isDNS bool) error {
		for _, v := range items {
			if err := ref("out", text(v["outbound"])); err != nil {
				return err
			}
			if isDNS {
				if err := ref("dns", text(v["server"])); err != nil {
					return err
				}
			}
			for _, k := range []string{"inbound", "rule_set"} {
				ids := stringsValue(v[k])
				if id := text(v[k]); id != "" {
					ids = []string{id}
				}
				for _, id := range ids {
					if err := ref(k, id); err != nil {
						return err
					}
				}
			}
			if err := rules(objects(v["rules"]), isDNS); err != nil {
				return err
			}
		}
		return nil
	}
	if err := rules(objects(r["rules"]), false); err != nil {
		return err
	}
	if err := rules(objects(d["rules"]), true); err != nil {
		return err
	}
	visited, active := map[string]bool{}, map[string]bool{}
	var walk func(string) error
	walk = func(id string) error {
		if active[id] {
			return fmt.Errorf("DNS/outbound dependency cycle at %s", id)
		}
		if visited[id] {
			return nil
		}
		active[id] = true
		for _, next := range graph[id] {
			if err := walk(next); err != nil {
				return err
			}
		}
		active[id] = false
		visited[id] = true
		return nil
	}
	for id := range graph {
		if err := walk(id); err != nil {
			return err
		}
	}
	return nil
}
