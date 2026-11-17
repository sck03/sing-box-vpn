package manage

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
)

type backupRequest struct {
	Password string   `json:"password"`
	Backup   *os.File `json:"-"`
	Apply    bool     `json:"-"`
	Revision int64    `json:"-"`
}

func (v *backupRequest) Close() {
	if v.Backup != nil {
		name := v.Backup.Name()
		v.Backup.Close()
		os.Remove(name)
		v.Backup = nil
	}
}

// Restore receives the encrypted file as binary multipart data. Browser and
// server no longer allocate a second Base64/JSON representation of the upload.
func readBackupRequest(w http.ResponseWriter, r *http.Request, v *backupRequest) error {
	if r.URL.Path == "/api/backup" {
		return decodeRequest(w, r, v, 4096)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPlatformBackupRequest)
	reader, err := r.MultipartReader()
	if err != nil {
		return errors.New("restore requires multipart/form-data")
	}
	seen := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := part.FormName()
		if seen[name] {
			part.Close()
			return errors.New("duplicate restore field")
		}
		seen[name] = true
		limit := int64(1024)
		if name == "backup" {
			file, e := os.CreateTemp("", "sbvpn-restore-*")
			if e != nil {
				part.Close()
				return e
			}
			v.Backup = file
			n, e := io.Copy(file, io.LimitReader(part, maxPlatformBackup+1))
			part.Close()
			if e != nil {
				return e
			}
			if n > maxPlatformBackup {
				return errors.New("restore field exceeds size limit")
			}
			if _, e = file.Seek(0, io.SeekStart); e != nil {
				return e
			}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, limit+1))
		part.Close()
		if err != nil {
			return err
		}
		if int64(len(data)) > limit {
			return errors.New("restore field exceeds size limit")
		}
		switch name {
		case "password":
			v.Password = string(data)
		case "apply":
			v.Apply, err = strconv.ParseBool(string(data))
		case "revision":
			v.Revision, err = strconv.ParseInt(string(data), 10, 64)
		default:
			return errors.New("unknown restore field")
		}
		if err != nil {
			return errors.New("invalid restore field")
		}
	}
	if !seen["backup"] || !seen["password"] {
		return errors.New("restore requires backup and password")
	}
	if v.Apply && v.Revision < 1 {
		return errors.New("restore apply requires a revision")
	}
	return nil
}
