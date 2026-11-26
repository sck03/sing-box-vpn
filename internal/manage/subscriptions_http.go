package manage

import (
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
	mu                       sync.Mutex
	configInfo, snapshotInfo os.FileInfo
	config                   sub.Config
	snapshot                 sub.Snapshot
	confirmedRevision        int64
}

func sameSubscriptionFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// The configuration lock gives a consistent view across snapshot and permission
// changes. Atomic inode replacement invalidates the cache even within one second.
func (v *subscriptionView) load(root string) error {
	paths := []string{subscriptionsConfig, subscriptionsSnapshot}
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
	if !sameSubscriptionFile(v.configInfo, infos[0]) {
		var c sub.Config
		if _, err := ReadJSON(filepath.Join(root, paths[0]), &c); err != nil {
			return err
		}
		if err := c.Validate(); err != nil {
			return err
		}
		v.config, v.configInfo = c, infos[0]
		v.confirmedRevision = -1
	}
	if !sameSubscriptionFile(v.snapshotInfo, infos[1]) {
		var snapshot sub.Snapshot
		if _, err := ReadJSON(filepath.Join(root, paths[1]), &snapshot); err != nil {
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
		v.snapshot, v.snapshotInfo = snapshot, infos[1]
	}
	return nil
}

// Revalidate the database generation on every request; compare the immutable
// configuration only when the publication or confirmed revision changes.
func (v *subscriptionView) confirm(ctx context.Context, root string, service AccountService) error {
	applied, revision, err := confirmedSubscriptionState(ctx, root, service)
	if err != nil {
		return err
	}
	if v.confirmedRevision != revision {
		if err := checkSubscriptionConfig(applied.Subscriptions, v.config); err != nil {
			return err
		}
		v.confirmedRevision = revision
	}
	return nil
}

func subscriptionHandlerView(service AccountService) http.Handler {
	if service.configuration == nil {
		service.configuration = &configurationCache{}
	}
	root, view, store := service.Root, service.view, service.Store
	if view == nil {
		view = &subscriptionView{}
	}
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
		if store == nil {
			http.Error(w, "subscription repository unavailable", 503)
			return
		}
		var err error
		err = withStartupLock(request.Context(), root, func() error {
			view.mu.Lock()
			defer view.mu.Unlock()
			if err := view.load(root); err != nil {
				return err
			}
			if err := view.confirm(request.Context(), root, service); err != nil {
				return err
			}
			grant, err := store.LookupGrant(request.Context(), parts[2])
			if err != nil || grant.AccountID != "" || !slices.Contains(grant.Outputs, parts[3]) || (grant.MaxDownloads != nil && grant.Downloads >= *grant.MaxDownloads) {
				status = http.StatusNotFound
				return errors.New("invalid authorization")
			}
			if err := outputPolicy(view.config, view.snapshot, parts[3]).check(request.Context(), store, time.Now()); err != nil {
				if errors.Is(err, errOutputAccountUnavailable) {
					status = http.StatusNotFound
				}
				return err
			}
			artifact = view.snapshot.Artifacts[parts[3]]
			// Only expose upstream quota when exactly one remote account contributes.
			refs := view.snapshot.Contributors[parts[3]]
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
		serveSubscriptionArtifact(w, request, artifact, func() error {
			return store.CountDownload(request.Context(), parts[2], parts[3])
		})
	})
}
