package manage

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Only existing immutable embedded assets enter this bounded cache. Readers
// access the embedded bytes directly instead of allocating a copy per request.
var panelETags sync.Map

func servePanelAsset(w http.ResponseWriter, r *http.Request, name string) {
	file, err := panelAssets.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	content, ok := file.(io.ReadSeeker)
	if !ok {
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
		etag, found := panelETags.Load(name)
		if !found {
			digest := sha256.New()
			if _, err = io.Copy(digest, content); err != nil {
				http.Error(w, "asset unavailable", http.StatusInternalServerError)
				return
			}
			if _, err = content.Seek(0, io.SeekStart); err != nil {
				http.Error(w, "asset unavailable", http.StatusInternalServerError)
				return
			}
			etag, _ = panelETags.LoadOrStore(name, fmt.Sprintf(`"%x"`, digest.Sum(nil)))
		}
		w.Header().Set("ETag", etag.(string))
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, name, time.Time{}, content)
}
