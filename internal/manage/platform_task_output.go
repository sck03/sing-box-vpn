package manage

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Preserve completed probe results even when execution exhausted its deadline.
// Result persistence has a separate, short deadline and still returns the cause.
func (s *PlatformService) storeTaskOutput(ctx context.Context, id int64, data []byte, operationErr error) error {
	if len(data) == 0 {
		return operationErr
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return errors.Join(operationErr, err)
	}
	redactPlatform(result)
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Join(operationErr, err)
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return errors.Join(operationErr, s.Store.Result(finish, id, data))
}
