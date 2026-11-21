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
	SourceHash     string       `json:"source_hash"`
	Nodes          []Node       `json:"nodes"`
	ETag           string       `json:"etag,omitempty"`
	Usage          Usage        `json:"usage"`
	Success        time.Time    `json:"success"`
	Checked        time.Time    `json:"checked"`
	Next           time.Time    `json:"next"`
	Failures       int          `json:"failures,omitempty"`
	Error          string       `json:"error,omitempty"`
	Diagnostics    []Diagnostic `json:"diagnostics,omitempty"`
	NotifiedExpiry int64        `json:"notified_expiry,omitempty"`
}

type ResourceCache struct {
	Checked  time.Time       `json:"checked,omitzero"`
	Success  time.Time       `json:"success,omitzero"`
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
	RefreshPending bool                          `json:"refresh_pending,omitempty"`
	Sources        map[string]SourceCache        `json:"sources"`
	Runs           []Run                         `json:"runs"`
	Resources      map[string]ResourceCache      `json:"resources,omitempty"`
	Deliveries     map[string]DeliveryState      `json:"deliveries,omitempty"`
	Accounts       map[string]AccountSourceState `json:"accounts,omitempty"`
}

type NotificationState struct {
	EndpointHash string    `json:"endpoint_hash,omitempty"`
	Checked      time.Time `json:"checked,omitzero"`
	Success      time.Time `json:"success,omitzero"`
	Failures     int       `json:"failures,omitempty"`
	Error        string    `json:"error,omitempty"`
	Event        string    `json:"event,omitempty"`
	ObjectID     string    `json:"object_id,omitempty"`
}

// AccountSourceState identifies rendering inputs, excluding rapidly changing
// traffic counters. Availability changes also cover expiry and quota renewal.
type AccountSourceState struct {
	Revision  int64 `json:"revision"`
	Available bool  `json:"available"`
}

type DeliveryState struct {
	ConfigHash string    `json:"config_hash"`
	Status     string    `json:"status"`
	Success    time.Time `json:"success,omitzero"`
	Digest     string    `json:"digest,omitempty"`
	Checked    time.Time `json:"checked"`
	Next       time.Time `json:"next"`
	Failures   int       `json:"failures,omitempty"`
	Error      string    `json:"error,omitempty"`
}

func DeliveryHash(d Delivery) string {
	data, _ := json.Marshal(d)
	return Digest(data)
}

type Snapshot struct {
	Version      int                     `json:"version"`
	Revision     uint64                  `json:"revision"`
	Artifacts    map[string]Artifact     `json:"artifacts"`
	Diagnostics  map[string][]Diagnostic `json:"diagnostics"`
	Usage        map[string]Usage        `json:"usage"`
	SourceTimes  map[string]time.Time    `json:"source_times"`
	CoreIdentity string                  `json:"core_identity,omitempty"`
	InputDigests map[string]string       `json:"input_digests,omitempty"`
	NodeCounts   map[string]int          `json:"node_counts,omitempty"`
	Templates    map[string]Template     `json:"templates,omitempty"`
	InputCounts  map[string]int          `json:"input_counts,omitempty"`
	NodeKeys     map[string][]string     `json:"node_keys,omitempty"`
	Validations  map[string]Validation   `json:"validations,omitempty"`
}

func SourceHash(source Source) string {
	data, _ := json.Marshal(struct {
		Parser int
		Source Source
	}{ParserVersion, source})
	return Digest(data)
}

func DefaultConfig() Config {
	c := Config{Version: SchemaVersion, Revision: 1, RuleSets: []RuleSet{}, Sources: []Source{}, Collections: []Collection{}, Outputs: []Output{}}
	for _, scope := range []string{"normal", "vpngate"} {
		for _, target := range []struct{ client, mode, suffix string }{{"sing-box", "config", "sing-box"}, {"mihomo", "config", "mihomo"}, {"openclash", "provider", "provider"}, {"base64", "nodes", "base64"}, {"uri", "nodes", "uri"}} {
			id := scope + "-" + target.suffix
			preset := "proxy"
			if scope == "normal" {
				preset = "split"
			}
			c.Templates = append(c.Templates, Template{ID: id, Version: 1, Client: target.client, Mode: target.mode, Preset: preset, TUN: target.client == "sing-box"})
		}
	}
	// Account outputs use built-ins until explicitly overridden. Do not copy
	// every profile into editable configuration just to express its default.
	return c
}

type BuildMode uint8

const (
	Strict BuildMode = iota
	Available
	Candidate
)

// Candidate tolerates unavailable runtime data, but never invalid output options.
// Runtime recovery alone may isolate an incompatible output to restore others.
func Compile(c Config, runtime Runtime, local map[string][]Node, now time.Time, mode BuildMode) (Snapshot, map[string]Preview, error) {
	return build(c, runtime, local, now, mode)
}

func CompileCached(c Config, runtime Runtime, local map[string][]Node, now time.Time, mode BuildMode, previous Snapshot) (Snapshot, map[string]Preview, error) {
	return buildCached(c, runtime, local, now, mode, previous)
}

func build(c Config, runtime Runtime, local map[string][]Node, now time.Time, mode BuildMode) (Snapshot, map[string]Preview, error) {
	return buildCached(c, runtime, local, now, mode, Snapshot{})
}

func buildCached(c Config, runtime Runtime, local map[string][]Node, now time.Time, mode BuildMode, previous Snapshot) (Snapshot, map[string]Preview, error) {
	available := mode != Strict
	var err error
	c, err = ResolveResources(c, runtime.Resources)
	if err != nil {
		return Snapshot{}, nil, err
	}
	result := Snapshot{Version: SchemaVersion, Revision: c.Revision, Artifacts: map[string]Artifact{}, Diagnostics: map[string][]Diagnostic{}, Usage: map[string]Usage{}, SourceTimes: map[string]time.Time{}}
	previews := map[string]Preview{}
	result.InputDigests = map[string]string{}
	result.NodeCounts = map[string]int{}
	result.InputCounts = map[string]int{}
	result.NodeKeys = map[string][]string{}
	result.Validations = map[string]Validation{}
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
		case "account":
			nodes = local["account:"+source.ID]
		case "inline":
			var parsed ParseResult
			parsed, err = ParseSource([]byte(source.Content), source)
			nodes = parsed.Nodes
			result.Diagnostics["source:"+source.ID] = parsed.Diagnostics
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
				result.Diagnostics["source:"+source.ID] = cached.Diagnostics
				result.Usage[source.ID], result.SourceTimes[source.ID] = cached.Usage, cached.Success
			}
		}
		if err == nil && source.Required && len(nodes) == 0 {
			err = errors.New("required source is empty")
		}
		if err != nil {
			if source.Required && (!available || (source.Kind != "remote" && source.Kind != "account")) {
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
		for _, sourceID := range collection.Sources {
			preview.Diagnostics = append(preview.Diagnostics, result.Diagnostics["source:"+sourceID]...)
		}
		previews[collection.ID] = preview
	}
	templates := EffectiveTemplates(c)
	result.Templates = templates
	total := 0
	for _, output := range c.Outputs {
		preview := previews[output.Collection]
		result.InputCounts[output.ID] = len(preview.Nodes)
		expanded += len(preview.Nodes)
		if expanded > MaxNodes*8 {
			return result, previews, errors.New("combined rendered node budget exceeded")
		}
		if len(preview.Nodes) == 0 {
			result.Diagnostics[output.ID] = append(append([]Diagnostic(nil), preview.Diagnostics...), Diagnostic{Stage: "selection", Message: "empty or unavailable collection; output unavailable"})
			continue
		}
		template := templates[output.Template]
		selected, diagnostics, err := Select(preview.Nodes, template, output.SkipUnsupported)
		var artifact Artifact
		if err == nil {
			input, marshalErr := json.Marshal(struct {
				Schema, Compiler int
				Nodes            []Node
				Template         Template
			}{SchemaVersion, CompilerVersion, selected, template})
			if marshalErr != nil {
				return result, previews, marshalErr
			}
			digest := Digest(input)
			old, exists := previous.Artifacts[output.ID]
			if previous.Version == SchemaVersion && exists && previous.InputDigests[output.ID] == digest && old.ETag == `"`+Digest(old.Data)+`"` {
				artifact = old
			} else {
				artifact, diagnostics, err = renderSelected(selected, template, Artifact{Name: template.ID}, diagnostics)
			}
			if err == nil {
				result.InputDigests[output.ID] = digest
			}
		}
		result.Diagnostics[output.ID] = append(append([]Diagnostic(nil), preview.Diagnostics...), diagnostics...)
		if err != nil {
			if mode == Available {
				result.Diagnostics[output.ID] = append(result.Diagnostics[output.ID], Diagnostic{Stage: "render", Message: err.Error()})
				continue
			}
			return result, previews, fmt.Errorf("output %s: %w", output.ID, err)
		}
		artifact.Name = output.Name
		result.NodeCounts[output.ID] = len(selected)
		keys := make([]string, len(selected))
		for i, node := range selected {
			keys[i] = Digest([]byte(node.Identity() + "\n" + node.Name))[:24]
		}
		result.NodeKeys[output.ID] = keys
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
