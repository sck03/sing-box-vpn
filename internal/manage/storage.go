package manage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const maxJSONFile = 16 << 20

type fileBuffer [32 << 10]byte

var fileBuffers = sync.Pool{New: func() any { return new(fileBuffer) }}

func ReadJSON(path string, target any) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("配置文件不是普通文件")
	}
	if info.Size() > maxJSONFile {
		return false, errors.New("配置文件超过 16 MiB")
	}
	// One allocation for an atomic, stable file. A concurrent growth is still
	// bounded by the original limit rather than trusting the initial size.
	data := make([]byte, int(info.Size())+1)
	n, err := io.ReadFull(file, data)
	data = data[:n]
	if err == nil && n <= maxJSONFile {
		var tail []byte
		tail, err = io.ReadAll(io.LimitReader(file, int64(maxJSONFile+1-n)))
		data = append(data, tail...)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	if len(data) > maxJSONFile {
		return false, errors.New("配置文件超过 16 MiB")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return true, errors.New("配置文件不能为 null")
	}
	return true, json.Unmarshal(data, target)
}

func jsonEncoder(output io.Writer) *json.Encoder {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder
}

func JSONBytes(value any) ([]byte, error) {
	var out bytes.Buffer
	err := jsonEncoder(&out).Encode(value)
	return out.Bytes(), err
}

func AtomicJSON(path string, value any) error {
	return atomicWrite(path, 0600, func(output io.Writer) error {
		return jsonEncoder(output).Encode(value)
	})
}

func AtomicBytes(path string, data []byte, mode os.FileMode) error {
	return atomicWrite(path, mode, func(output io.Writer) error {
		n, err := output.Write(data)
		if err == nil && n != len(data) {
			return io.ErrShortWrite
		}
		return err
	})
}

func atomicWrite(path string, mode os.FileMode, write func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		err = write(file)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

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
