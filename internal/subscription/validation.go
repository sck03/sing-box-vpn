package subscription

type Validation struct {
	State               string `json:"state"`
	Engine              string `json:"engine"`
	Scope               string `json:"scope"`
	Cached              bool   `json:"cached"`
	ApplicationVerified bool   `json:"application_verified"`
}

func (v Validation) String() string {
	text := v.Engine + " " + v.Scope + ": " + v.State
	if v.Cached {
		text += " (cached)"
	}
	return text
}
