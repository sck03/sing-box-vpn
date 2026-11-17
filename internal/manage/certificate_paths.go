package manage

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
