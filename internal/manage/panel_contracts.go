package manage

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
