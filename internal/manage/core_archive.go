package manage

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
)

const maxCoreExpanded = 512 << 20

// These files live in a private, disposable download directory. They are not
// publication journals and do not need an atomic rewrite or a directory sync.
func writeCoreStage(path string, mode os.FileMode, write func(io.Writer) error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	err = write(file)
	return errors.Join(err, file.Close())
}

// Bound every decompressed byte, including tar metadata, skipped entries and
// trailing gzip members. Checking only the selected entry misses all of these.
type archiveReader struct {
	ctx       context.Context
	source    io.Reader
	remaining int64
}

func (r *archiveReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.source.Read(p[:min(int64(len(p)), r.remaining+1)])
	if int64(n) > r.remaining {
		return 0, errors.New("核心归档解压后超过 512 MiB")
	}
	r.remaining -= int64(n)
	return n, err
}

func extractCore(ctx context.Context, input io.Reader, output io.Writer, expected string) error {
	gz, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer gz.Close()
	bounded := &archiveReader{ctx: ctx, source: gz, remaining: maxCoreExpanded}
	reader := tar.NewReader(bounded)
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Size < 0 || header.Size > bounded.remaining {
			return errors.New("归档文件超过剩余解压预算")
		}
		if header.Name != expected {
			// Drain explicitly with the shared buffer instead of tar's private
			// discard buffer, and keep cancellation active while skipping entries.
			if _, err := copyFileContent(io.Discard, reader); err != nil {
				return err
			}
			continue
		}
		if found || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size <= 0 || header.Size > maxAssetSize {
			return errors.New("归档中的核心文件无效或重复")
		}
		if _, err := copyFileContent(output, reader); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return errors.New("归档缺少预期核心文件")
	}
	// tar EOF can precede gzip EOF. Consume the trailer to verify CRC/length
	// and reject truncation before running or publishing the extracted binary.
	_, err = copyFileContent(io.Discard, bounded)
	return err
}
