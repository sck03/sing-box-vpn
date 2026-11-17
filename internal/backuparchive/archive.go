// Package backuparchive owns the bounded, authenticated backup wire format.
// It knows nothing about SQL schemas, HTTP, certificate providers or services.
package backuparchive

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	strict "github.com/sck03/sing-box-vpn/internal/encoding"
)

const (
	Magic       = "SBVPNV07"
	MaxArchive  = 96 << 20
	MaxDatabase = 80 << 20
	MaxFiles    = 2048
	MaxFile     = 1 << 20
	workFactor  = 16
)

type Manifest struct {
	Version int       `json:"version"`
	Root    string    `json:"root"`
	Created time.Time `json:"created"`
}

type Archive struct {
	Manifest
	Database []byte
	Files    map[string][]byte
}

func validFile(name string) bool {
	return name != "." && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../") && path.Clean(name) == name && !strings.ContainsAny(name, "\\:\x00") && (strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key"))
}

type boundedWriter struct {
	ctx       context.Context
	w         io.Writer
	remaining int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		return 0, errors.New("backup exceeds 96 MiB")
	}
	n, err := w.w.Write(p)
	w.remaining -= int64(n)
	return n, err
}

// Write streams immutable staged files through tar and age. Database bytes are
// never copied into a JSON envelope or a second full-size ciphertext buffer.
func Write(ctx context.Context, dst io.Writer, password []byte, manifest Manifest, database string, files map[string]string) error {
	if len(password) < 12 || len(password) > 1024 || len(files) > MaxFiles {
		return errors.New("invalid backup password or file count")
	}
	manifest.Version = 7
	recipient, err := age.NewScryptRecipient(string(password))
	if err != nil {
		return err
	}
	recipient.SetWorkFactor(workFactor)
	w := &boundedWriter{ctx: ctx, w: dst, remaining: MaxArchive}
	if _, err = io.WriteString(w, Magic); err != nil {
		return err
	}
	encrypted, err := age.Encrypt(w, recipient)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(encrypted)
	meta, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(meta) > 4096 {
		return errors.New("backup manifest exceeds budget")
	}
	if err = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(meta))}); err != nil {
		return err
	}
	if _, err = tw.Write(meta); err != nil {
		return err
	}
	writeFile := func(name, filename string, limit int64) error {
		f, e := os.Open(filename)
		if e != nil {
			return e
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() > limit {
			return errors.New("backup entry exceeds budget or is not a regular file")
		}
		if e = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: info.Size()}); e != nil {
			return e
		}
		_, e = io.CopyN(tw, f, info.Size())
		return e
	}
	if err = writeFile("control.db", database, MaxDatabase); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !validFile(name) {
			return errors.New("invalid certificate archive path")
		}
		if err = writeFile("files/"+name, files[name], MaxFile); err != nil {
			return err
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	return encrypted.Close()
}

func Read(data, password []byte) (Archive, error) {
	result := Archive{Files: map[string][]byte{}}
	if len(password) < 12 || len(password) > 1024 || len(data) > MaxArchive || !bytes.HasPrefix(data, []byte(Magic)) {
		return result, errors.New("invalid format-7 encrypted backup")
	}
	identity, err := age.NewScryptIdentity(string(password))
	if err != nil {
		return result, err
	}
	identity.SetMaxWorkFactor(workFactor)
	decrypted, err := age.Decrypt(bytes.NewReader(data[len(Magic):]), identity)
	if err != nil {
		return result, errors.New("backup password incorrect or archive damaged")
	}
	bounded := &io.LimitedReader{R: decrypted, N: MaxArchive + 1}
	tr := tar.NewReader(bounded)
	seen := map[string]bool{}
	for {
		h, e := tr.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return result, e
		}
		if seen[h.Name] || h.Typeflag != tar.TypeReg || len(seen) >= MaxFiles+2 {
			return result, errors.New("invalid or duplicate backup entry")
		}
		seen[h.Name] = true
		limit := int64(MaxFile)
		switch h.Name {
		case "manifest.json":
			limit = 4096
		case "control.db":
			limit = MaxDatabase
		default:
			if !strings.HasPrefix(h.Name, "files/") || !validFile(strings.TrimPrefix(h.Name, "files/")) {
				return result, errors.New("invalid certificate backup entry")
			}
		}
		if h.Size < 0 || h.Size > limit {
			return result, errors.New("backup entry exceeds budget")
		}
		value := make([]byte, int(h.Size))
		_, e = io.ReadFull(tr, value)
		if e != nil {
			return result, e
		}
		switch h.Name {
		case "manifest.json":
			if e = strict.JSON(value, &result.Manifest, 4096); e != nil {
				return result, e
			}
		case "control.db":
			result.Database = value
		default:
			result.Files[strings.TrimPrefix(h.Name, "files/")] = value
		}
	}
	// tar EOF is not encrypted EOF: consume the authentication trailer before
	// trusting any restored bytes, including a corrupted final age chunk.
	if _, err = io.Copy(io.Discard, bounded); err != nil {
		return result, err
	}
	if bounded.N <= 0 || !seen["manifest.json"] || len(result.Database) == 0 || result.Version != 7 || result.Root == "" {
		return result, errors.New("unsupported or incomplete backup")
	}
	return result, nil
}
