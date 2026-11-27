package domain

import (
	"errors"
	"regexp"
)

type TaskSource string

const (
	TaskUser      TaskSource = "user"
	TaskCLI       TaskSource = "cli"
	TaskScheduler TaskSource = "scheduler"
)

// TaskSubmission is trusted admission metadata, never part of a public command.
// Actor is an audit identity; Source alone controls scheduled-task coalescing.
type TaskSubmission struct {
	Actor       string
	Source      TaskSource
	RequestID   string
	Fingerprint string
}

var taskRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
var taskFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s TaskSubmission) Validate() error {
	if s.Actor == "" || len(s.Actor) > 128 {
		return errors.New("invalid task actor")
	}
	switch s.Source {
	case TaskUser, TaskCLI, TaskScheduler:
	default:
		return errors.New("invalid task source")
	}
	if s.RequestID == "" && s.Fingerprint == "" {
		return nil
	}
	if s.Source != TaskUser || !taskRequestID.MatchString(s.RequestID) || !taskFingerprint.MatchString(s.Fingerprint) {
		return errors.New("invalid task request identity")
	}
	return nil
}
