package manage

import (
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type AccountSave struct {
	Account domain.Account `json:"account"`
}

type SubscriptionActionOptions struct {
	Revision   int64                `json:"revision"`
	Pending    bool                 `json:"pending"`
	Outputs    []application.Option `json:"outputs"`
	Deliveries []application.Option `json:"deliveries"`
}

type OutboundOption struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type OutboundOptions struct {
	Revision int64            `json:"revision"`
	Pending  bool             `json:"pending"`
	Items    []OutboundOption `json:"items"`
}
type AccountArchive struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}
type ShareCreated struct {
	Token   string   `json:"token"`
	UserURL string   `json:"user_url"`
	URLs    []string `json:"urls"`
}

type OverviewSnapshot struct {
	Background      []BackgroundIssue                  `json:"background"`
	Revision        int64                              `json:"revision"`
	AppliedRevision int64                              `json:"applied_revision"`
	Services        []ServiceState                     `json:"services"`
	Runtimes        []controlstore.RuntimeState        `json:"runtimes"`
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

type AccountExportResponse struct {
	Content    string                  `json:"content"`
	MIME       string                  `json:"mime"`
	Extension  string                  `json:"extension"`
	Output     sub.AccountOutputOption `json:"output"`
	Validation sub.Validation          `json:"validation"`
}
