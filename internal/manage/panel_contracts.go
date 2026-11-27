package manage

import (
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type AccountSave struct {
	Account domain.Account `json:"account"`
}

type ActionResult struct {
	OK bool `json:"ok"`
}

type PanelSettingsInput struct {
	Options  PanelOptions `json:"options"`
	Revision string       `json:"revision,omitempty"`
}

type PanelSettingsState struct {
	Options         PanelOptions `json:"options"`
	Active          PanelOptions `json:"active"`
	Revision        string       `json:"revision"`
	RestartRequired bool         `json:"restart_required"`
}

type PanelSettingsPreview struct {
	Options         PanelOptions `json:"options"`
	RestartRequired bool         `json:"restart_required"`
}

type PanelSettingsSaved struct {
	OK              bool   `json:"ok"`
	RestartRequired bool   `json:"restart_required"`
	PublicURL       string `json:"public_url"`
}

type AdministratorInput struct {
	Admin    adminauth.Admin `json:"admin,omitempty"`
	ID       string          `json:"id,omitempty"`
	Password string          `json:"password,omitempty"`
	Code     string          `json:"code,omitempty"`
}

type MFAEnrollment struct {
	Secret         string `json:"secret"`
	Issuer         string `json:"issuer"`
	Account        string `json:"account"`
	ExpiresSeconds int    `json:"expires_seconds"`
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

type GrantRevoke struct {
	ID string `json:"id"`
}

type OverviewSnapshot struct {
	ResourceVersion uint64                             `json:"resource_version"`
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
type InboundPreparation struct {
	ID       string         `json:"id"`
	Protocol string         `json:"protocol"`
	Config   PlatformConfig `json:"config"`
}

type CountryPreparation struct {
	Country string         `json:"country"`
	Config  PlatformConfig `json:"config"`
}

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
