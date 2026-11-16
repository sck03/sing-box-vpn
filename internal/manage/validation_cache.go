package manage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

var coreIdentities = struct {
	sync.Mutex
	entries map[string]struct {
		info   os.FileInfo
		digest string
	}
}{entries: map[string]struct {
	info   os.FileInfo
	digest string
}{}}

// Hash a core once per process/file identity. Receipts remain valid across CLI
// invocations, but never across a changed binary (including the same version).
func coreIdentity(binary string) (string, error) {
	path, err := filepath.Abs(binary)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	coreIdentities.Lock()
	defer coreIdentities.Unlock()
	if old, ok := coreIdentities.entries[path]; ok && os.SameFile(old.info, info) && old.info.Size() == info.Size() && old.info.ModTime() == info.ModTime() {
		return old.digest, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if len(coreIdentities.entries) >= 32 {
		clear(coreIdentities.entries)
	}
	coreIdentities.entries[path] = struct {
		info   os.FileInfo
		digest string
	}{info, digest}
	return digest, nil
}

func validationKey(binary string, value any) (string, error) {
	identity, err := coreIdentity(binary)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = io.WriteString(h, identity)
	_, _ = h.Write(data)
	// Certificates and private keys can change without a config text change.
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		return "", err
	}
	paths := map[string]bool{}
	var visit func(any)
	visit = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, value := range v {
				if key == "certificate_path" || key == "key_path" {
					if path, ok := value.(string); ok && path != "" {
						paths[path] = true
					}
				}
				visit(value)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(tree)
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		data, err := readSubscriptionFile(path)
		if err != nil {
			return "", fmt.Errorf("validation dependency: %w", err)
		}
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validatedConfig(root, key string) bool {
	var keys []string
	if _, err := ReadJSON(filepath.Join(root, ".validation.json"), &keys); err != nil {
		return false
	}
	for _, old := range keys {
		if old == key {
			return true
		}
	}
	return false
}

func rememberValidation(root, key string) {
	var keys []string
	_, _ = ReadJSON(filepath.Join(root, ".validation.json"), &keys)
	if len(keys) >= 128 {
		keys = keys[len(keys)-127:]
	}
	keys = append(keys, key)
	// Losing the optional receipt only causes a fresh core check next time.
	_ = AtomicJSON(filepath.Join(root, ".validation.json"), keys)
}
