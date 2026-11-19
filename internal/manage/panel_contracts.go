package manage

import (
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

type OverviewSnapshot struct {
	Revision        int64                              `json:"revision"`
	AppliedRevision int64                              `json:"applied_revision"`
	Services        []Object                           `json:"services"`
	Runtimes        []map[string]any                   `json:"runtimes"`
	Tasks           []controlstore.Task                `json:"tasks"`
	TaskCounts      controlstore.TaskCounts            `json:"task_counts"`
	Credentials     []accounting.CredentialPublication `json:"credentials"`
}

// Named wire contracts are shared by handlers and the frontend type generator.
type ConfigurationSave struct {
	Revision int64          `json:"revision"`
	Config   PlatformConfig `json:"config"`
}

type ConfigurationState struct {
	Revision        int64          `json:"revision"`
	AppliedRevision int64          `json:"applied_revision"`
	Config          PlatformConfig `json:"config"`
	Root            string         `json:"root"`
}

type QueuedTask struct {
	TaskID int64 `json:"task_id"`
}
