package manage

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/sck03/sing-box-vpn/internal/application"
)

func pageQuery(r *http.Request) (application.PageQuery, error) {
	v := r.URL.Query()
	if d := v.Get("direction"); d != "" && d != "asc" && d != "desc" {
		return application.PageQuery{}, errors.New("invalid sort direction")
	}
	q := application.PageQuery{Search: v.Get("q"), Limit: 20, Sort: v.Get("sort"), Descending: v.Get("direction") == "desc"}
	var err error
	if raw := v.Get("offset"); raw != "" {
		q.Offset, err = strconv.Atoi(raw)
		if err != nil {
			return q, err
		}
	}
	if raw := v.Get("limit"); raw != "" {
		q.Limit, err = strconv.Atoi(raw)
		if err != nil {
			return q, err
		}
	}
	return q, q.Validate()
}
