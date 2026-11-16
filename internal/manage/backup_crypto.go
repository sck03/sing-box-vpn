package manage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha256"
)

func backupCipher(password, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, string(password), salt, 600000, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func mapAppCertificates(app *App, visit func(string) (string, error)) error {
	var err error
	for _, path := range []*string{&app.Profile.Certificate, &app.Profile.Key} {
		if *path != "" {
			*path, err = visit(*path)
			if err != nil {
				return err
			}
		}
	}
	for _, in := range app.Inbounds {
		tls := object(in["tls"])
		for _, key := range []string{"certificate_path", "key_path"} {
			if value := text(tls[key]); value != "" {
				tls[key], err = visit(value)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}
