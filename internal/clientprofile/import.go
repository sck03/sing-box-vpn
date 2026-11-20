package clientprofile

// ImportPrefix contains only documented direct application schemes. Provider
// fragments cannot be installed as complete device configurations.
func ImportPrefix(client, mode string) string {
	if client == "stash" && mode == "config" {
		return "stash://install-config?url="
	}
	return ""
}
