package domain

import "errors"

type PageQuery struct {
	Search     string
	Offset     int
	Limit      int
	Sort       string
	Descending bool
}

func (q PageQuery) Validate() error {
	if q.Offset < 0 || q.Offset > 100000 || q.Limit < 1 || q.Limit > 100 || len(q.Search) > 256 {
		return errors.New("invalid pagination; limit must be 1–100")
	}
	return nil
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

type Option struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
