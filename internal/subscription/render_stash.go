package subscription

import "fmt"

func fromStash(m Object) (Node, error) {
	if str(m["type"]) == "hysteria2" {
		if _, exists := m["password"]; exists {
			return Node{}, fmt.Errorf("Stash Hysteria2 requires auth, not password")
		}
		m["password"] = m["auth"]
		delete(m, "auth")
	}
	if str(m["type"]) == "tuic" {
		if version := integer(m["version"]); version != 0 && version != 5 {
			return Node{}, fmt.Errorf("only Stash TUIC v5 is supported")
		}
		delete(m, "version")
	}
	return fromMihomo(m)
}

func stashNode(n Node) Object {
	m := n.Mihomo()
	if n.Protocol == "hysteria2" {
		m["auth"] = n.Password
		delete(m, "password")
	}
	if n.Protocol == "tuic" {
		m["version"] = 5
	}
	// Stash has its own schema; only its documented common fields are emitted.
	delete(m, "client-fingerprint")
	delete(m, "fingerprint")
	return m
}
