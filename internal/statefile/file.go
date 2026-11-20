// Package statefile owns bounded JSON reads and durable atomic file writes.
package statefile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const MaxJSONFile = 16 << 20

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
	if info.Size() > MaxJSONFile {
		return false, errors.New("配置文件超过 16 MiB")
	}
	// One allocation for an atomic, stable file. A concurrent growth is still
	// bounded by the original limit rather than trusting the initial size.
	data := make([]byte, int(info.Size())+1)
	n, err := io.ReadFull(file, data)
	data = data[:n]
	if err == nil && n <= MaxJSONFile {
		var tail []byte
		tail, err = io.ReadAll(io.LimitReader(file, int64(MaxJSONFile+1-n)))
		data = append(data, tail...)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	if len(data) > MaxJSONFile {
		return false, errors.New("配置文件超过 16 MiB")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return true, errors.New("配置文件不能为 null")
	}
	return true, json.Unmarshal(data, target)
}

func JSONEncoder(output io.Writer) *json.Encoder {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder
}

func JSONBytes(value any) ([]byte, error) {
	var out bytes.Buffer
	err := JSONEncoder(&out).Encode(value)
	return out.Bytes(), err
}

func AtomicJSON(path string, value any) error {
	return AtomicWrite(path, 0600, func(output io.Writer) error {
		return JSONEncoder(output).Encode(value)
	})
}

func AtomicBytes(path string, data []byte, mode os.FileMode) error {
	return AtomicWrite(path, mode, func(output io.Writer) error {
		n, err := output.Write(data)
		if err == nil && n != len(data) {
			return io.ErrShortWrite
		}
		return err
	})
}

func AtomicWrite(path string, mode os.FileMode, write func(io.Writer) error) error {
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
	return SyncDirectory(filepath.Dir(path))
}
