package manage

type fileBundle map[string][]byte

const normalScope = "normal"
const vpngateScope = "vpngate"

func (files fileBundle) json(name string, value any) error {
	data, err := JSONBytes(value)
	if err == nil {
		files[name] = data
	}
	return err
}
