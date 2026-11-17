package manage

import (
	"errors"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"io"
	"os"
)

func readSubscriptionFile(path string) ([]byte, error) {
	return readLimitedRegularFile(path, sub.MaxBody)
}

func readLimitedRegularFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("input must be a regular file within the size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("input file too large")
	}
	return data, nil
}
