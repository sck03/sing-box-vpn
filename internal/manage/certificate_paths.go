package manage

func mapAppCertificates(app *App, visit func(string) (string, error)) error {
	for i := range app.Nodes {
		for _, path := range []*string{&app.Nodes[i].TLS.Certificate, &app.Nodes[i].TLS.Key} {
			if *path != "" {
				value, err := visit(*path)
				if err != nil {
					return err
				}
				*path = value
			}
		}
	}
	return nil
}
