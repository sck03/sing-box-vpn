package manage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/statefile"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const maxJSONFile = 16 << 20

type fileBuffer [32 << 10]byte

var fileBuffers = sync.Pool{New: func() any { return new(fileBuffer) }}

func ReadJSON(path string, target any) (bool, error) { return statefile.ReadJSON(path, target) }
func jsonEncoder(output io.Writer) *json.Encoder     { return statefile.JSONEncoder(output) }
func JSONBytes(value any) ([]byte, error)            { return statefile.JSONBytes(value) }
func AtomicJSON(path string, value any) error        { return statefile.AtomicJSON(path, value) }
func AtomicBytes(path string, data []byte, mode os.FileMode) error {
	return statefile.AtomicBytes(path, data, mode)
}
func atomicWrite(path string, mode os.FileMode, write func(io.Writer) error) error {
	return statefile.AtomicWrite(path, mode, write)
}
func syncDirectory(path string) error { return statefile.SyncDirectory(path) }

// A private draft is invisible until its journal is durable. Write it once;
// preparePublish syncs its directory tree together after all files are ready.
func writeStaged(path string, write func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = write(file)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func copyFileContent(output io.Writer, input io.Reader) (int64, error) {
	buffer := fileBuffers.Get().(*fileBuffer)
	defer fileBuffers.Put(buffer)
	// Suppress both fast-path interfaces: File.ReadFrom can otherwise bypass the
	// borrowed buffer and allocate another one after File.WriteTo is hidden.
	return io.CopyBuffer(struct{ io.Writer }{output}, struct{ io.Reader }{input}, buffer[:])
}

func fileEquals(file *os.File, info os.FileInfo, expected []byte) (bool, error) {
	if info.Size() != int64(len(expected)) {
		return false, nil
	}
	buffer := fileBuffers.Get().(*fileBuffer)
	defer fileBuffers.Put(buffer)
	for offset := 0; offset < len(expected); {
		size := min(len(buffer), len(expected)-offset)
		if _, err := io.ReadFull(file, buffer[:size]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return false, nil
			}
			return false, err
		}
		if !bytes.Equal(buffer[:size], expected[offset:offset+size]) {
			return false, nil
		}
		offset += size
	}
	_, err := file.Read(buffer[:1])
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

func fingerprintFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("校验文件不是普通文件: %s", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := copyFileContent(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// atomicCopy publishes staged material without allocating a file-sized buffer.
func atomicCopy(destination, source string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	return atomicWrite(destination, 0600, func(w io.Writer) error { _, err := copyFileContent(w, file); return err })
}
