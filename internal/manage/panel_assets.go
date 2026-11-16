package manage

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func servePanelAsset(w http.ResponseWriter, r *http.Request, name string) {
	data, err := panelAssets.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mime := "text/html; charset=utf-8"
	if strings.HasSuffix(name, ".js") {
		mime = "text/javascript; charset=utf-8"
	}
	if strings.HasSuffix(name, ".css") {
		mime = "text/css; charset=utf-8"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css") {
		// Revalidate the complete module graph after upgrades; unchanged assets
		// return 304 without transferring a second copy of the file.
		w.Header().Set("Cache-Control", "public, no-cache")
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256(data)))
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
