package manage

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Only existing immutable embedded assets enter this bounded cache. Readers
// access the embedded bytes directly instead of allocating a copy per request.
var panelETags sync.Map

type panelAsset struct {
	once sync.Once
	etag string
	gzip []byte
	err  error
}

func acceptsGzip(header string) bool {
	wildcard := false
	for _, value := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(value), ";")
		quality := 1.0
		for _, option := range parts[1:] {
			if key, v, ok := strings.Cut(strings.TrimSpace(option), "="); ok && key == "q" {
				var err error
				quality, err = strconv.ParseFloat(v, 64)
				if err != nil || quality < 0 || quality > 1 {
					quality = 0
				}
			}
		}
		if strings.EqualFold(parts[0], "gzip") {
			return quality > 0
		}
		if parts[0] == "*" {
			wildcard = quality > 0
		}
	}
	return wildcard
}

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
		cached, _ := panelETags.LoadOrStore(name, &panelAsset{})
		asset := cached.(*panelAsset)
		asset.once.Do(func() {
			var raw []byte
			raw, asset.err = io.ReadAll(content)
			if asset.err != nil {
				return
			}
			asset.etag = fmt.Sprintf(`"%x"`, sha256.Sum256(raw))
			var compressed bytes.Buffer
			writer, _ := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
			_, asset.err = writer.Write(raw)
			if err := writer.Close(); asset.err == nil {
				asset.err = err
			}
			asset.gzip = compressed.Bytes()
		})
		if asset.err != nil {
			http.Error(w, "asset unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("ETag", asset.etag)
		if acceptsGzip(r.Header.Get("Accept-Encoding")) && r.Header.Get("Range") == "" {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("ETag", strings.TrimSuffix(asset.etag, `"`)+`-gzip"`)
			content = bytes.NewReader(asset.gzip)
		} else if _, err = content.Seek(0, io.SeekStart); err != nil {
			http.Error(w, "asset unavailable", http.StatusInternalServerError)
			return
		}
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, name, time.Time{}, content)
}
