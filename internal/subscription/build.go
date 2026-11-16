package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Usage struct {
	Upload   *uint64 `json:"upload,omitempty"`
	Download *uint64 `json:"download,omitempty"`
	Total    *uint64 `json:"total,omitempty"`
	Expire   *int64  `json:"expire,omitempty"`
}

type SourceCache struct {
	SourceHash     string    `json:"source_hash"`
	Nodes          []Node    `json:"nodes"`
	ETag           string    `json:"etag,omitempty"`
	Usage          Usage     `json:"usage"`
	Success        time.Time `json:"success"`
	Checked        time.Time `json:"checked"`
	Next           time.Time `json:"next"`
	Failures       int       `json:"failures,omitempty"`
	Error          string    `json:"error,omitempty"`
	NotifiedExpiry int64     `json:"notified_expiry,omitempty"`
}

type ResourceCache struct {
	Body     json.RawMessage `json:"body,omitempty"`
	URL      string          `json:"url"`
	ETag     string          `json:"etag,omitempty"`
	Next     time.Time       `json:"next"`
	Failures int             `json:"failures,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type Run struct {
	Time      time.Time `json:"time"`
	Operation string    `json:"operation"`
	ObjectID  string    `json:"object_id,omitempty"`
	Message   string    `json:"message"`
}

type Runtime struct {
	Sources    map[string]SourceCache   `json:"sources"`
	Runs       []Run                    `json:"runs"`
	Resources  map[string]ResourceCache `json:"resources,omitempty"`
	Deliveries map[string]DeliveryState `json:"deliveries,omitempty"`
}

type DeliveryState struct {
	Digest   string    `json:"digest,omitempty"`
	Checked  time.Time `json:"checked"`
	Next     time.Time `json:"next"`
	Failures int       `json:"failures,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type Snapshot struct {
	Version      int                     `json:"version"`
	Revision     uint64                  `json:"revision"`
	Artifacts    map[string]Artifact     `json:"artifacts"`
	Diagnostics  map[string][]Diagnostic `json:"diagnostics"`
	Usage        map[string]Usage        `json:"usage"`
	SourceTimes  map[string]time.Time    `json:"source_times"`
	CoreIdentity string                  `json:"core_identity,omitempty"`
}

func SourceHash(source Source) string { data, _ := json.Marshal(source); return Digest(data) }

func DefaultConfig() Config {
	c := Config{Version: SchemaVersion, Revision: 1, RuleSets: []RuleSet{}}
	for _, scope := range []string{"normal", "vpngate"} {
		c.Sources = append(c.Sources, Source{ID: scope, Name: scope, Kind: "local", Scope: scope, Required: scope == "normal"})
		c.Collections = append(c.Collections, Collection{ID: scope, Name: scope, Sources: []string{scope}, Processor: Processor{Deduplicate: true}})
		for _, target := range []struct{ client, mode, suffix string }{{"sing-box", "config", "sing-box"}, {"mihomo", "config", "mihomo"}, {"openclash", "provider", "provider"}, {"base64", "nodes", "base64"}, {"uri", "nodes", "uri"}} {
			id := scope + "-" + target.suffix
			preset := "proxy"
			if scope == "normal" {
				preset = "split"
			}
			c.Templates = append(c.Templates, Template{ID: id, Version: 1, Client: target.client, Mode: target.mode, Preset: preset, TUN: target.client == "sing-box"})
			c.Outputs = append(c.Outputs, Output{ID: id, Name: id, Collection: scope, Template: id})
		}
	}
	return c
}

// Build uses already fetched data. Network activity is never part of publishing
// or a download request. A required stale/missing source aborts the whole build.
func Build(c Config, runtime Runtime, local map[string][]Node, now time.Time) (Snapshot, map[string]Preview, error) {
	return build(c, runtime, local, now, false)
}

// BuildAvailable removes outputs whose required runtime dependencies are unavailable.
// Local maintenance and background refresh can publish unrelated valid outputs;
// explicit configuration edits still use Build to reject an incomplete plan.
func BuildAvailable(c Config, runtime Runtime, local map[string][]Node, now time.Time) (Snapshot, map[string]Preview, error) {
	return build(c, runtime, local, now, true)
}

func build(c Config, runtime Runtime, local map[string][]Node, now time.Time, available bool) (Snapshot, map[string]Preview, error) {
	var err error
	c, err = ResolveResources(c, runtime.Resources)
	if err != nil {
		return Snapshot{}, nil, err
	}
	result := Snapshot{Version: SchemaVersion, Revision: c.Revision, Artifacts: map[string]Artifact{}, Diagnostics: map[string][]Diagnostic{}, Usage: map[string]Usage{}, SourceTimes: map[string]time.Time{}}
	previews := map[string]Preview{}
	if err := c.Validate(); err != nil {
		return result, previews, err
	}
	sources := map[string][]Node{}
	unavailable := map[string]bool{}
	nodeCount := 0
	for _, source := range c.Sources {
		var nodes []Node
		var err error
		switch source.Kind {
		case "local":
			nodes = local[source.Scope]
		case "account":
			nodes = local["account:"+source.ID]
		case "inline":
			nodes, err = Parse([]byte(source.Content), source.Format, source.ID)
		case "remote":
			cached, exists := runtime.Sources[source.ID]
			maxStale := source.MaxStaleSeconds
			if maxStale == 0 {
				maxStale = 86400
			}
			if !exists || cached.SourceHash != SourceHash(source) || cached.Success.IsZero() || now.Sub(cached.Success) > time.Duration(maxStale)*time.Second {
				err = errors.New("no fresh successful source cache")
			} else {
				nodes = cached.Nodes
				result.Usage[source.ID], result.SourceTimes[source.ID] = cached.Usage, cached.Success
			}
		}
		if err == nil && source.Required && len(nodes) == 0 {
			err = errors.New("required source is empty")
		}
		if err != nil {
			if source.Required && (!available || (source.Kind != "remote" && source.Kind != "account" && source.Kind != "local")) {
				return result, previews, fmt.Errorf("source %s: %w", source.ID, err)
			}
			unavailable[source.ID] = source.Required
			result.Diagnostics["source:"+source.ID] = []Diagnostic{{Stage: "source", Message: err.Error()}}
			continue
		}
		nodeCount += len(nodes)
		if nodeCount > MaxNodes {
			return result, previews, errors.New("combined source node limit exceeded")
		}
		sources[source.ID] = nodes
	}
	expanded := 0
	for _, collection := range c.Collections {
		count := 0
		missing := false
		for _, id := range collection.Sources {
			count += len(sources[id])
			missing = missing || unavailable[id]
		}
		if missing {
			previews[collection.ID] = Preview{Diagnostics: []Diagnostic{{Stage: "source", Message: "required source unavailable"}}}
			continue
		}
		expanded += count
		if count > MaxNodes || expanded > MaxNodes*4 {
			return result, previews, errors.New("expanded collection node budget exceeded")
		}
		nodes := make([]Node, 0, count)
		for _, id := range collection.Sources {
			nodes = append(nodes, sources[id]...)
		}
		preview, err := Process(nodes, collection.Processor)
		if err != nil {
			return result, previews, fmt.Errorf("collection %s: %w", collection.ID, err)
		}
		previews[collection.ID] = preview
	}
	templates := map[string]Template{}
	ruleSets := map[string][]Rule{}
	for _, r := range c.RuleSets {
		ruleSets[r.ID] = r.Rules
	}
	for _, t := range c.Templates {
		t.Rules = append([]Rule(nil), t.Rules...)
		for _, id := range t.RuleSets {
			t.Rules = append(t.Rules, ruleSets[id]...)
		}
		templates[t.ID] = t
	}
	total := 0
	for _, output := range c.Outputs {
		preview := previews[output.Collection]
		expanded += len(preview.Nodes)
		if expanded > MaxNodes*8 {
			return result, previews, errors.New("combined rendered node budget exceeded")
		}
		if len(preview.Nodes) == 0 {
			result.Diagnostics[output.ID] = append(append([]Diagnostic(nil), preview.Diagnostics...), Diagnostic{Stage: "selection", Message: "empty or unavailable collection; output unavailable"})
			continue
		}
		artifact, diagnostics, err := Render(preview.Nodes, templates[output.Template], output.SkipUnsupported)
		result.Diagnostics[output.ID] = append(append([]Diagnostic(nil), preview.Diagnostics...), diagnostics...)
		if err != nil {
			if available {
				result.Diagnostics[output.ID] = append(result.Diagnostics[output.ID], Diagnostic{Stage: "render", Message: err.Error()})
				continue
			}
			return result, previews, fmt.Errorf("output %s: %w", output.ID, err)
		}
		artifact.Name = output.Name
		result.Artifacts[output.ID] = artifact
		total += len(artifact.Data)
		if total > MaxBody {
			return result, previews, errors.New("combined output limit exceeded")
		}
	}
	return result, previews, nil
}

func (r *Runtime) Record(operation, id, message string, now time.Time) {
	if len(message) > 512 {
		message = message[:512]
	}
	r.Runs = append(r.Runs, Run{now, operation, id, message})
	if len(r.Runs) > 100 {
		r.Runs = append([]Run(nil), r.Runs[len(r.Runs)-100:]...)
	}
}
