package manage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func platformOwned(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".platform-v4"))
	return err == nil
}

func recoverConfig(ctx context.Context, root string) error {
	return withStartupLock(ctx, root, func() error { return nil })
}

func validateImportedCertificate(certPath, keyPath, domain string) error {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	cert := pair.Leaf
	if cert == nil {
		cert, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return err
		}
	}
	if err := cert.VerifyHostname(domain); err != nil {
		return fmt.Errorf("证书不包含域名 %s: %w", domain, err)
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return errors.New("证书尚未生效或已过期，请先续期")
	}
	return nil
}

func ensureObject(m Object, key string) Object {
	if value := object(m[key]); value != nil {
		return value
	}
	value := Object{}
	m[key] = value
	return value
}

func subscriptionHandler(root string, stores ...*accounting.Store) http.Handler {
	view := &subscriptionView{}
	if len(stores) > 0 {
		return subscriptionHandlerView(root, view, stores[0])
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store, err := accounting.Open(filepath.Join(root, "control.db"))
		if err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		defer store.Close()
		subscriptionHandlerView(root, view, store).ServeHTTP(w, r)
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
