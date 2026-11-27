package manage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

// Only the HTTP boundary constructs user metadata. Scheduling policy is never
// decoded from a command. Hash the endpoint and decoded DTO, not raw JSON order.
func taskSubmission(r *http.Request, actor string, body any) (domain.TaskSubmission, error) {
	submission := domain.TaskSubmission{Actor: actor, Source: domain.TaskUser, RequestID: r.Header.Get("Idempotency-Key")}
	if submission.RequestID != "" {
		data, err := json.Marshal(body)
		if err != nil {
			return submission, err
		}
		digest := sha256.Sum256(append([]byte(r.URL.Path+"\n"), data...))
		submission.Fingerprint = hex.EncodeToString(digest[:])
	}
	return submission, submission.Validate()
}
