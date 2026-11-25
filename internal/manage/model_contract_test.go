package manage

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/serverconfig"
)

func TestManagedNodeRoundTripPreservesTransport(t *testing.T) {
	_, _, app := appFixture(t)
	for i := range app.Nodes {
		n := &app.Nodes[i]
		if n.Protocol != "vmess" {
			continue
		}
		n.Transport.EarlyData, n.Transport.EarlyDataHeader = 4096, "X-Client-Early-Data"
		n.Transport.Path, n.Transport.Host = "/custom", "edge.example.com"
	}
	raw, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	var saved App
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(saved)
	if !bytes.Equal(raw, again) {
		t.Fatal("typed configuration changed on persistence")
	}
	core, err := serverForApp(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range objects(core["inbounds"]) {
		if in["type"] != "vmess" {
			continue
		}
		transport := object(in["transport"])
		if transport["max_early_data"] != 4096 || transport["early_data_header_name"] != "X-Client-Early-Data" || transport["path"] != "/custom" {
			t.Fatal("core projection lost supported transport fields", transport)
		}
	}
	// Core projections are detached: downstream rendering cannot change authority.
	objects(core["inbounds"])[0]["listen_port"] = 1
	after, _ := json.Marshal(saved)
	if !bytes.Equal(raw, after) {
		t.Fatal("core mutation changed stored nodes")
	}
}

func TestManagedNodeRejectsUnsupportedFields(t *testing.T) {
	_, _, app := appFixture(t)
	for i := range app.Nodes {
		if app.Nodes[i].Protocol == "vmess" {
			app.Nodes[i].Transport.Type = "grpc"
		}
	}
	_, err := serverForApp(app)
	var field *serverconfig.FieldError
	if !errors.As(err, &field) || field.Field != "transport.type" {
		t.Fatal("unsupported transport silently accepted", err)
	}
	raw, _ := json.Marshal(app)
	raw = bytes.Replace(raw, []byte(`"type":"grpc"`), []byte(`"type":"ws","service_name":"unsupported"`), 1)
	var decoded App
	if json.Unmarshal(raw, &decoded) == nil {
		t.Fatal("unknown transport field accepted")
	}
	raw, _ = json.Marshal(app)
	raw = bytes.Replace(raw, []byte(`"schema":5`), []byte(`"schema":4`), 1)
	if json.Unmarshal(raw, &decoded) == nil {
		t.Fatal("old node schema accepted")
	}
}

func TestPanelErrorContract(t *testing.T) {
	for _, tc := range []struct {
		err         error
		status      int
		code, field string
	}{
		{adminauth.ErrConflict, 409, "revision_conflict", ""},
		{&serverconfig.FieldError{Node: "node", Field: "transport.type", Message: "ws required"}, 400, "invalid_field", "app.nodes[node].transport.type"},
	} {
		response := httptest.NewRecorder()
		platformError(response, tc.err)
		var value struct {
			Error APIError `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if response.Code != tc.status || value.Error.Code != tc.code || value.Error.Field != tc.field || value.Error.Message == "" {
			t.Fatal(response.Code, value)
		}
	}
}

func TestAccountServiceRequiresExplicitRepositories(t *testing.T) {
	s := AccountService{Root: t.TempDir()}
	if _, err := s.requireStore(); err == nil {
		t.Fatal("missing repository silently opened")
	}
	if _, err := s.configSnapshot(t.Context()); err == nil {
		t.Fatal("missing configuration repository silently opened")
	}
}
