package manage

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionView struct {
	mu                                     sync.Mutex
	configInfo, snapshotInfo, settingsInfo os.FileInfo
	config                                 sub.Config
	snapshot                               sub.Snapshot
	settings                               subscriptionSettings
}

func sameSubscriptionFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// The configuration lock gives a consistent view across snapshot and permission
// changes. Atomic inode replacement invalidates the cache even within one second.
func (v *subscriptionView) load(root string) error {
	paths := []string{"subscription.json", subscriptionsConfig, subscriptionsSnapshot}
	infos := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		file, err := os.Open(filepath.Join(root, path))
		if err != nil {
			return err
		}
		// File.Stat captures the file identity immediately on Windows; os.Stat
		// defers that lookup until SameFile, after the path may be replaced.
		info, err := file.Stat()
		file.Close()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxJSONFile {
			return errors.New("invalid subscription state")
		}
		infos[i] = info
	}
	if !sameSubscriptionFile(v.settingsInfo, infos[0]) {
		var settings subscriptionSettings
		if _, err := ReadJSON(filepath.Join(root, paths[0]), &settings); err != nil {
			return err
		}
		v.settings, v.settingsInfo = settings, infos[0]
	}
	if !sameSubscriptionFile(v.configInfo, infos[1]) {
		var c sub.Config
		if _, err := ReadJSON(filepath.Join(root, paths[1]), &c); err != nil {
			return err
		}
		if err := c.Validate(); err != nil {
			return err
		}
		v.config, v.configInfo = c, infos[1]
	}
	if !sameSubscriptionFile(v.snapshotInfo, infos[2]) {
		var snapshot sub.Snapshot
		if _, err := ReadJSON(filepath.Join(root, paths[2]), &snapshot); err != nil {
			return err
		}
		if snapshot.Version != sub.SchemaVersion || len(snapshot.Artifacts) > sub.MaxObjects {
			return errors.New("invalid snapshot version or size")
		}
		total := 0
		for _, a := range snapshot.Artifacts {
			total += len(a.Data)
			if len(a.Data) == 0 || total > sub.MaxBody || a.ETag != `"`+sub.Digest(a.Data)+`"` {
				return errors.New("invalid subscription artifact")
			}
		}
		v.snapshot, v.snapshotInfo = snapshot, infos[2]
	}
	return nil
}

func subscriptionFresh(c sub.Config, snapshot sub.Snapshot, outputID string, now time.Time) bool {
	var collection string
	for _, output := range c.Outputs {
		if output.ID == outputID {
			collection = output.Collection
		}
	}
	var sources []string
	for _, set := range c.Collections {
		if set.ID == collection {
			sources = set.Sources
		}
	}
	for _, source := range c.Sources {
		if source.Kind != "remote" || !slices.Contains(sources, source.ID) {
			continue
		}
		last, exists := snapshot.SourceTimes[source.ID]
		if !exists {
			if source.Required {
				return false
			}
			continue
		}
		limit := source.MaxStaleSeconds
		if limit == 0 {
			limit = 86400
		}
		if now.Sub(last) > time.Duration(limit)*time.Second {
			return false
		}
	}
	return true
}

func matchesETag(header, tag string) bool {
	for value := range strings.SplitSeq(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || strings.TrimPrefix(value, "W/") == tag {
			return true
		}
	}
	return false
}

func subscriptionHandler(root string) http.Handler {
	view := &subscriptionView{}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", 405)
			return
		}
		parts := strings.Split(request.URL.Path, "/")
		if len(parts) != 4 || parts[1] != "s" || !subscriptionTokenPattern.MatchString(parts[2]) || !sub.ValidID(parts[3]) || request.URL.RawQuery != "" {
			http.NotFound(w, request)
			return
		}
		status := http.StatusServiceUnavailable
		var artifact sub.Artifact
		var usage *sub.Usage
		err := withStartupLock(request.Context(), root, func() error {
			view.mu.Lock()
			defer view.mu.Unlock()
			if err := view.load(root); err != nil {
				return err
			}
			if !view.settings.Enabled {
				status = http.StatusNotFound
				return errors.New("subscription disabled")
			}
			grant, ok := findGrant(view.config, parts[2], parts[3], time.Now())
			if !ok {
				status = http.StatusNotFound
				return errors.New("invalid authorization")
			}
			var found bool
			artifact, found = view.snapshot.Artifacts[parts[3]]
			if !found || !subscriptionFresh(view.config, view.snapshot, parts[3], time.Now()) {
				return errors.New("subscription unavailable")
			}
			if grant.MaxDownloads > 0 {
				var count uint64
				usagePath := filepath.Join(root, "subscriptions", "usage", grant.TokenHash+".json")
				if _, err := ReadJSON(usagePath, &count); err != nil {
					return err
				}
				if count >= grant.MaxDownloads {
					status = http.StatusNotFound
					return errors.New("download allowance exhausted")
				}
				if request.Method == http.MethodGet && !matchesETag(request.Header.Get("If-None-Match"), artifact.ETag) {
					if err := AtomicJSON(usagePath, count+1); err != nil {
						return err
					}
				}
			}
			// Only expose upstream quota when exactly one remote account contributes.
			var refs []string
			var collection string
			for _, out := range view.config.Outputs {
				if out.ID == parts[3] {
					collection = out.Collection
				}
			}
			for _, set := range view.config.Collections {
				if set.ID == collection {
					refs = set.Sources
				}
			}
			if len(refs) == 1 {
				if value, found := view.snapshot.Usage[refs[0]]; found {
					copy := value
					usage = &copy
				}
			}
			status = http.StatusOK
			return nil
		})
		if err != nil {
			if status == 404 {
				http.NotFound(w, request)
			} else {
				http.Error(w, "subscription unavailable", status)
			}
			return
		}
		w.Header().Set("Content-Type", artifact.MIME+"; charset=utf-8")
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("ETag", artifact.ETag)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("profile-update-interval", "1")
		if usage != nil {
			w.Header().Set("subscription-userinfo", formatSubscriptionUsage(*usage))
		}
		http.ServeContent(w, request, parts[3], time.Time{}, bytes.NewReader(artifact.Data))
	})
}

func subscriptionReadSnapshot(ctx context.Context, root string) (sub.Snapshot, error) {
	var snapshot sub.Snapshot
	err := withStartupLock(ctx, root, func() error {
		found, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("no published subscriptions")
		}
		return nil
	})
	return snapshot, err
}
