package subscription

import (
	"errors"
	"slices"
)

// OnlyOutput retains the selected output's source graph. Rule sets remain
// available because a fetched template can reference different local rule IDs.
func (c Config) OnlyOutput(id string) (Config, error) {
	index := slices.IndexFunc(c.Outputs, func(o Output) bool { return o.ID == id })
	if index < 0 {
		return Config{}, errors.New("unknown subscription output")
	}
	o := c.Outputs[index]
	ci := slices.IndexFunc(c.Collections, func(v Collection) bool { return v.ID == o.Collection })
	ti := slices.IndexFunc(c.Templates, func(v Template) bool { return v.ID == o.Template })
	if ci < 0 || ti < 0 {
		return Config{}, errors.New("output has missing dependencies")
	}
	collection := c.Collections[ci]
	selected := c
	selected.Outputs = []Output{o}
	selected.Collections = []Collection{collection}
	selected.Templates = []Template{c.Templates[ti]}
	selected.Sources = nil
	for _, source := range c.Sources {
		if slices.Contains(collection.Sources, source.ID) {
			selected.Sources = append(selected.Sources, source)
		}
	}
	selected.AccountDefaults = nil
	selected.AccountOverrides = nil
	selected.Deliveries = nil
	return selected, nil
}

type SelectionDelta struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

func CompareSelection(before, after []string) SelectionDelta {
	old := make(map[string]bool, len(before))
	for _, key := range before {
		old[key] = true
	}
	next := make(map[string]bool, len(after))
	result := SelectionDelta{}
	for _, key := range after {
		next[key] = true
		if !old[key] {
			result.Added++
		}
	}
	for key := range old {
		if !next[key] {
			result.Removed++
		}
	}
	return result
}
