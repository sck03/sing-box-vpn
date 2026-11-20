package manage

// Bound allocations and external validators without starving status, tasks or SSE.
func expensivePanelRequest(path, method string) bool {
	if method != "POST" {
		return false
	}
	switch path {
	case "/api/config", "/api/config/preview", "/api/config/preview-output", "/api/accounts/export", "/api/grants", "/api/subscription-templates/import", "/api/backup", "/api/restore":
		return true
	}
	return false
}
