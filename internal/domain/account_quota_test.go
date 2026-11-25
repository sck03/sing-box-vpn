package domain

import "testing"

func TestAccountQuotaRoundTripsThroughJSONNumbers(t *testing.T) {
	for _, quota := range []int64{0, MaxQuotaBytes, MaxQuotaBytes + 1, -1} {
		err := (Account{ID: "quota", Name: "Quota", QuotaBytes: &quota}).Validate()
		if (err == nil) != (quota >= 0 && quota <= MaxQuotaBytes) {
			t.Fatalf("quota=%d, error=%v", quota, err)
		}
	}
}
