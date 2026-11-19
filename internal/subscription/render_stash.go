package subscription

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
