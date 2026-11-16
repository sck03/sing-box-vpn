package domain

import (
	"testing"
	"time"
)

func TestMonthlyCalendarBoundaries(t *testing.T) {
	p := PeriodPolicy{Kind: "monthly", Day: 31, Timezone: "Asia/Shanghai"}
	anchor := time.Date(2024, 1, 31, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for _, tc := range []struct{ now, start, next string }{
		{"2024-02-28T16:00:00Z", "2024-02-28T16:00:00Z", "2024-03-30T16:00:00Z"},
		{"2024-03-29T16:00:00Z", "2024-02-28T16:00:00Z", "2024-03-30T16:00:00Z"},
		{"2025-02-28T16:00:00Z", "2025-02-27T16:00:00Z", "2025-03-30T16:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		start, next := p.Bounds(anchor, now)
		if start.Format(time.RFC3339) != tc.start || next == nil || next.Format(time.RFC3339) != tc.next {
			t.Fatalf("%s: %s %v", tc.now, start, next)
		}
	}
}
func TestCalendarDaysAcrossDST(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	p := PeriodPolicy{Kind: "days", Days: 1, Timezone: loc.String()}
	anchor := time.Date(2024, 3, 9, 12, 0, 0, 0, loc)
	start, next := p.Bounds(anchor, anchor.Add(23*time.Hour))
	if start.In(loc).Day() != 10 || start.In(loc).Hour() != 12 || next.In(loc).Day() != 11 {
		t.Fatal(start, next)
	}
	if start.Sub(anchor) != 23*time.Hour {
		t.Fatal("used fixed 24-hour days")
	}
}
