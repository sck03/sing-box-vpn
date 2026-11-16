package domain

import (
	"errors"
	"time"
	_ "time/tzdata"
)

// PeriodPolicy is a calendar policy, independent of transport and persistence.
// Days are local calendar days, not multiples of 24 hours across DST changes.
type PeriodPolicy struct {
	Kind     string `json:"kind"`
	Days     int    `json:"days,omitempty"`
	Day      int    `json:"day,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

func (p PeriodPolicy) Validate() error {
	if p.Kind == "" || p.Kind == "none" {
		return nil
	}
	if p.Kind != "days" && p.Kind != "monthly" {
		return errors.New("period kind must be none, days or monthly")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil || p.Timezone == "" {
		return errors.New("period requires an IANA timezone")
	}
	if p.Kind == "days" && (p.Days < 1 || p.Days > 3660) {
		return errors.New("period days must be 1–3660")
	}
	if p.Kind == "monthly" && (p.Day < 1 || p.Day > 31) {
		return errors.New("monthly day must be 1–31")
	}
	return nil
}

// Bounds always derives boundaries from the activation anchor. Clamping day 31
// in February therefore cannot accidentally change March's reset day to 28.
func (p PeriodPolicy) Bounds(anchor, now time.Time) (start time.Time, next *time.Time) {
	start = anchor.UTC()
	if p.Kind == "" || p.Kind == "none" || now.Before(anchor) {
		return
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return
	}
	a, n := anchor.In(loc), now.In(loc)
	var end time.Time
	if p.Kind == "days" && p.Days > 0 {
		// UTC civil dates yield a day index unaffected by local DST offsets.
		civil := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
		days := int(civil(n).Sub(civil(a)).Hours() / 24)
		index := max(0, days/p.Days)
		start = a.AddDate(0, 0, index*p.Days)
		if start.After(now) && index > 0 {
			index--
			start = a.AddDate(0, 0, index*p.Days)
		}
		end = a.AddDate(0, 0, (index+1)*p.Days)
	} else if p.Kind == "monthly" && p.Day > 0 {
		boundary := func(y int, m time.Month) time.Time {
			last := time.Date(y, m+1, 0, 0, 0, 0, 0, loc).Day()
			return time.Date(y, m, min(p.Day, last), 0, 0, 0, 0, loc)
		}
		candidate := boundary(n.Year(), n.Month())
		if candidate.After(now) {
			candidate = boundary(n.Year(), n.Month()-1)
		}
		if candidate.After(anchor) {
			start = candidate
		}
		end = boundary(n.Year(), n.Month())
		if !end.After(now) {
			end = boundary(n.Year(), n.Month()+1)
		}
	} else {
		return
	}
	start = start.UTC()
	end = end.UTC()
	next = &end
	return
}
