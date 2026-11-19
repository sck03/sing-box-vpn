package subscription

import (
	"errors"
	"time"
)

func hoppingInterval(value string) (int, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 5*time.Second || duration > 24*time.Hour || duration%time.Second != 0 {
		return 0, errors.New("hop_interval requires 5–86400 whole seconds")
	}
	return int(duration / time.Second), nil
}
