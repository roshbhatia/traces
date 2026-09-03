// Package source reads spans from the collector and external provider commands.
//
// An activity.read provider is any executable that prints newline delimited
// JSON on stdout and exits. One line is one span:
//
//	{"traceId":"…","spanId":"…","parentId":"…","name":"…",
//	 "startUnixNano":"…","endUnixNano":"…",
//	 "attrs":{"service.name":"…","session.id":"…"}}
//
// Only traceId, spanId, name and the two stamps are required. service and
// session come off the attributes when the line omits them, so a provider that
// forwards the attributes it already has needs no extra fields.
//
// A line carrying an event is a log record instead, which is how a harness
// reports what it cannot put on a span:
//
//	{"event":"user_prompt","startUnixNano":"…",
//	 "attrs":{"service.name":"…","session.id":"…","prompt":"…"}}
//
// A line traces cannot parse is skipped rather than fatal, because a provider
// that prints a warning should not take the view down with it.
//
// Traces renders the action argv and environment from the provider/v1 manifest,
// runs the provider once per poll, and deduplicates by span ID. A provider is
// therefore stateless: it answers "which spans ended in the last N", and Traces
// decides what is new.
//
// TRACES_PROVIDER takes a comma-separated list. The provider output is one
// shared contract, so any source can add spans, messages, edits, or events
// without changing the UI.
package source

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	sharedprovider "github.com/roshbhatia/go-utils/provider"
	"github.com/roshbhatia/traces/internal/otlp"
)

// Env names the provider without a flag, so a machine can carry the choice in
// its own configuration rather than in every command line.
const Env = "TRACES_PROVIDER"

const (
	ActionActivityRead    = "activity.read"
	ActionSessionCurrent  = "session.current"
	ActionSessionDiscover = "session.discover"
	ActionDiffRender      = "diff.render"
)

var supportedActionNames = []string{
	ActionActivityRead,
	ActionDiffRender,
	ActionSessionCurrent,
	ActionSessionDiscover,
}

// Registry is the discovered provider set indexed by manifest name.
type Registry map[string]sharedprovider.LoadedManifest

type Provider struct {
	Manifest sharedprovider.Manifest
	Path     string
	// Name is what the caller asked for, for error text and the header.
	Name string
	// Session narrows the read when the provider can do it. traces resolves a
	// prefix itself, so a provider may ignore this.
	Session string
	// Directory lets providers find workspace-scoped sources such as commits.
	Directory string
	// Color is the caller's output policy for rendering actions.
	Color string
	// harnesses are the services this source answers for. It is empty on a
	// source the caller named directly, which is a deliberate escape hatch and
	// is never scoped.
	harnesses []string
}

type actionData struct {
	Since     string
	Session   string
	Directory string
	Local     string
	Remote    string
	Merged    string
	Width     int
	Color     string
}

// ValidationCheck describes one deterministic provider contract check.
type ValidationCheck struct {
	Message string `json:"message"`
	Name    string `json:"name"`
	Status  string `json:"status"`
}

// Validation is the complete result for one configured provider.
type Validation struct {
	Checks   []ValidationCheck `json:"checks"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Command  []string          `json:"command,omitempty"`
	Provides []string          `json:"provides"`
}

func validationCheck(name, message string, ok bool) ValidationCheck {
	status := "failed"
	if ok {
		status = "ok"
	}
	return ValidationCheck{Name: name, Message: message, Status: status}
}

// Validate resolves one provider and probes every declared standard action
// without opening the interactive UI or reading the caller's session state.
func Validate(ctx context.Context, name string, loaded sharedprovider.LoadedManifest, directory string) Validation {
	manifest := loaded.Manifest
	provides := make([]string, 0, len(manifest.Actions))
	for action := range manifest.Actions {
		provides = append(provides, action)
	}
	sort.Strings(provides)
	result := Validation{Name: name, Provides: provides, Command: append([]string{}, manifest.Command...)}
	report := (sharedprovider.Validator{}).Validate(manifest, directory)
	for _, check := range report.Checks {
		result.Checks = append(result.Checks, validationCheck(
			check.Kind+":"+check.Target, check.Message, check.Status == sharedprovider.CheckOK,
		))
	}
	if !report.OK() {
		result.Status = "failed"
		return result
	}
	if err := validateSupportedActions(manifest); err != nil {
		result.Checks = append(result.Checks, validationCheck("manifest:actions", err.Error(), false))
		result.Status = "failed"
		return result
	}
	result.Status = "ok"
	temporary, err := os.MkdirTemp("", "traces-provider-validation-")
	if err != nil {
		result.Checks = append(result.Checks, validationCheck("fixture", err.Error(), false))
		result.Status = "failed"
		return result
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	workspace := filepath.Join(temporary, "workspace")
	home := filepath.Join(temporary, "home")
	for _, path := range []string{
		workspace,
		home,
		filepath.Join(temporary, "cache"),
		filepath.Join(temporary, "config"),
		filepath.Join(temporary, "data"),
		filepath.Join(temporary, "tmp"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			result.Checks = append(result.Checks, validationCheck("fixture", err.Error(), false))
			result.Status = "failed"
			return result
		}
	}
	environment := validationEnvironment(temporary, manifest.Requires.Environment)
	fixture := actionData{
		Since: "0s", Session: "traces-provider-validation", Directory: workspace,
		Local: filepath.Join(temporary, "old.txt"), Remote: filepath.Join(temporary, "new.txt"),
		Merged: filepath.Join(temporary, "example.txt"), Width: 80, Color: "always",
	}
	if err := os.WriteFile(fixture.Local, []byte("old\n"), 0o600); err != nil {
		result.Checks = append(result.Checks, validationCheck("fixture", err.Error(), false))
		result.Status = "failed"
		return result
	}
	if err := os.WriteFile(fixture.Remote, []byte("new\n"), 0o600); err != nil {
		result.Checks = append(result.Checks, validationCheck("fixture", err.Error(), false))
		result.Status = "failed"
		return result
	}
	plans := make(map[string]sharedprovider.Plan, len(provides))
	for _, action := range provides {
		plan, err := manifest.Render(action, fixture)
		if err != nil {
			result.Checks = append(result.Checks, validationCheck("template:"+action, err.Error(), false))
			result.Status = "failed"
			continue
		}
		plans[action] = plan
	}
	for _, action := range provides {
		plan, ok := plans[action]
		if !ok {
			continue
		}
		message, err := validateAction(ctx, action, plan, workspace, environment)
		if err != nil {
			result.Checks = append(result.Checks, validationCheck("probe:"+action, err.Error(), false))
			result.Status = "failed"
			continue
		}
		result.Checks = append(result.Checks, validationCheck("probe:"+action, message, true))
	}
	return result
}

func validateAction(
	ctx context.Context,
	action string,
	plan sharedprovider.Plan,
	directory string,
	environment []string,
) (string, error) {
	if plan.Timeout <= 0 {
		plan.Timeout = 10 * time.Second
	}
	switch action {
	case ActionActivityRead:
		output, stderr, err := runPlanWithEnvironment(ctx, plan, directory, environment)
		if err != nil {
			return "", providerCommandError(stderr, err)
		}
		if err := validateOutput(output); err != nil {
			return "", err
		}
		if len(bytes.TrimSpace(output)) == 0 {
			return "accepted empty activity for a zero-length window", nil
		}
		return "returned valid newline-delimited activity", nil
	case ActionSessionCurrent, ActionSessionDiscover:
		output, stderr, err := runPlanWithEnvironment(ctx, plan, directory, environment)
		if err != nil {
			return "", providerCommandError(stderr, err)
		}
		limit := 0
		if action == ActionSessionCurrent {
			limit = 1
		}
		count, err := validateSessionOutput(output, limit)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("returned %d valid session identifiers", count), nil
	case ActionDiffRender:
		return validateDiff(ctx, plan, directory, environment)
	default:
		return "", fmt.Errorf("unsupported action %q", action)
	}
}

func validateSupportedActions(manifest sharedprovider.Manifest) error {
	allowed := make(map[string]bool, len(supportedActionNames))
	for _, name := range supportedActionNames {
		allowed[name] = true
	}
	for name := range manifest.Actions {
		if !allowed[name] {
			return fmt.Errorf("provider %q advertises unsupported action %q", manifest.Name, name)
		}
	}
	return nil
}

// ProviderSchema narrows the shared provider format to Traces capabilities.
func ProviderSchema() ([]byte, error) {
	data, err := sharedprovider.Schema()
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decode provider schema: %w", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("provider schema has no properties")
	}
	actions, ok := properties["actions"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("provider schema has no actions")
	}
	actions["propertyNames"] = map[string]any{"enum": supportedActionNames}
	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode provider schema: %w", err)
	}
	return append(encoded, '\n'), nil
}

func providerCommandError(stderr string, err error) error {
	if message := strings.TrimSpace(stderr); message != "" {
		return errors.New(message)
	}
	return err
}

func validateSessionOutput(output []byte, limit int) (int, error) {
	if !utf8.Valid(output) {
		return 0, fmt.Errorf("session output is not valid UTF-8")
	}
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		if strings.IndexFunc(id, unicode.IsControl) >= 0 {
			return 0, fmt.Errorf("session identifier contains a control character")
		}
		count++
		if limit > 0 && count > limit {
			return 0, fmt.Errorf("session.current returned more than one identifier")
		}
	}
	return count, nil
}

func validateDiff(
	ctx context.Context,
	plan sharedprovider.Plan,
	directory string,
	environment []string,
) (string, error) {
	output, stderr, err := runPlanWithEnvironment(ctx, plan, directory, environment)
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", providerCommandError(stderr, err)
		}
	}
	if strings.TrimSpace(string(output)) == "" {
		return "", fmt.Errorf("provider returned empty diff output")
	}
	return "rendered a deterministic two-file diff", nil
}

func validationEnvironment(root string, required []string) []string {
	environment := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"TMPDIR":          filepath.Join(root, "tmp"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_DATA_DIRS":   filepath.Join(root, "data-dirs"),
	}
	if path := os.Getenv("PATH"); path != "" {
		environment["PATH"] = path
	}
	for _, name := range required {
		if value, ok := os.LookupEnv(name); ok {
			environment[name] = value
		}
	}
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+environment[key])
	}
	return result
}

func validateOutput(output []byte) error {
	rows := bufio.NewScanner(bytes.NewReader(output))
	rows.Buffer(make([]byte, 1<<20), 1<<26)
	lineNumber := 0
	for rows.Scan() {
		lineNumber++
		text := bytes.TrimSpace(rows.Bytes())
		if len(text) == 0 {
			continue
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(text, &value); err != nil || value == nil {
			return fmt.Errorf("line %d is not a JSON object", lineNumber)
		}
		if _, spans := value["resourceSpans"]; spans {
			if err := validateOTLPExport(value, lineNumber); err != nil {
				return err
			}
			continue
		}
		if _, logs := value["resourceLogs"]; logs {
			if err := validateOTLPExport(value, lineNumber); err != nil {
				return err
			}
			continue
		}
		if _, event := value["event"]; event {
			if err := validateFlatEvent(text, lineNumber, value); err != nil {
				return err
			}
			continue
		}
		if _, span := value["spanId"]; span {
			if err := validateFlatSpan(text, lineNumber, value); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("line %d is neither a span, event, nor OTLP export", lineNumber)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read output: %w", err)
	}
	return nil
}

func validateFlatSpan(text []byte, lineNumber int, value map[string]json.RawMessage) error {
	var span line
	if err := json.Unmarshal(text, &span); err != nil {
		return fmt.Errorf("line %d has an invalid span field type: %w", lineNumber, err)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "traceId", value: span.TraceID},
		{name: "spanId", value: span.SpanID},
		{name: "name", value: span.Name},
		{name: "startUnixNano", value: span.Start},
		{name: "endUnixNano", value: span.End},
	} {
		if field.value == "" {
			return fmt.Errorf("line %d span requires non-empty %s", lineNumber, field.name)
		}
	}
	if err := validateStamp(span.Start); err != nil {
		return fmt.Errorf("line %d span startUnixNano: %w", lineNumber, err)
	}
	if err := validateStamp(span.End); err != nil {
		return fmt.Errorf("line %d span endUnixNano: %w", lineNumber, err)
	}
	return validateAttrs(value, lineNumber)
}

func validateFlatEvent(text []byte, lineNumber int, value map[string]json.RawMessage) error {
	var event line
	if err := json.Unmarshal(text, &event); err != nil {
		return fmt.Errorf("line %d has an invalid event field type: %w", lineNumber, err)
	}
	if event.Event == "" {
		return fmt.Errorf("line %d event requires non-empty event", lineNumber)
	}
	if event.Start == "" {
		return fmt.Errorf("line %d event requires non-empty startUnixNano", lineNumber)
	}
	if err := validateStamp(event.Start); err != nil {
		return fmt.Errorf("line %d event startUnixNano: %w", lineNumber, err)
	}
	return validateAttrs(value, lineNumber)
}

func validateAttrs(value map[string]json.RawMessage, lineNumber int) error {
	raw, ok := value["attrs"]
	if !ok {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("line %d attrs must be an object of strings", lineNumber)
	}
	var attrs map[string]string
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return fmt.Errorf("line %d attrs must be an object of strings: %w", lineNumber, err)
	}
	return nil
}

func validateStamp(value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return fmt.Errorf("must be a decimal nanosecond string")
	}
	return nil
}

type validationValue struct {
	StringValue *string  `json:"stringValue"`
	IntValue    *string  `json:"intValue"`
	BoolValue   *bool    `json:"boolValue"`
	DoubleValue *float64 `json:"doubleValue"`
	ArrayValue  *struct {
		Values []validationValue `json:"values"`
	} `json:"arrayValue"`
}

type validationKeyValue struct {
	Key   string          `json:"key"`
	Value validationValue `json:"value"`
}

type validationResource struct {
	Attributes []validationKeyValue `json:"attributes"`
}

type validationLogRecord struct {
	TraceID    string               `json:"traceId"`
	SpanID     string               `json:"spanId"`
	Time       string               `json:"timeUnixNano"`
	Observed   string               `json:"observedTimeUnixNano"`
	EventName  string               `json:"eventName"`
	Body       validationValue      `json:"body"`
	Attributes []validationKeyValue `json:"attributes"`
}

type validationSpan struct {
	TraceID      string               `json:"traceId"`
	SpanID       string               `json:"spanId"`
	ParentSpanID string               `json:"parentSpanId"`
	Name         string               `json:"name"`
	Start        string               `json:"startTimeUnixNano"`
	End          string               `json:"endTimeUnixNano"`
	Attributes   []validationKeyValue `json:"attributes"`
	Status       struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

type validationExport struct {
	ResourceLogs []struct {
		Resource  validationResource `json:"resource"`
		ScopeLogs []struct {
			LogRecords []validationLogRecord `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
	ResourceSpans []struct {
		Resource   validationResource `json:"resource"`
		ScopeSpans []struct {
			Spans []validationSpan `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

func validateOTLPExport(value map[string]json.RawMessage, lineNumber int) error {
	for _, field := range []string{"resourceSpans", "resourceLogs"} {
		raw, ok := value[field]
		if !ok {
			continue
		}
		var rows []json.RawMessage
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &rows) != nil {
			return fmt.Errorf("line %d %s must be an array", lineNumber, field)
		}
	}
	blob, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("line %d cannot read OTLP export: %w", lineNumber, err)
	}
	var exported validationExport
	if err := json.Unmarshal(blob, &exported); err != nil {
		return fmt.Errorf("line %d has an invalid OTLP field type: %w", lineNumber, err)
	}
	for resourceIndex, resource := range exported.ResourceLogs {
		for scopeIndex, scope := range resource.ScopeLogs {
			for recordIndex, record := range scope.LogRecords {
				position := fmt.Sprintf(
					"line %d OTLP log record %d/%d/%d", lineNumber, resourceIndex, scopeIndex, recordIndex,
				)
				if record.Time == "" && record.Observed == "" {
					return fmt.Errorf("%s requires timeUnixNano or observedTimeUnixNano", position)
				}
				for _, stamp := range []struct {
					name  string
					value string
				}{
					{name: "timeUnixNano", value: record.Time},
					{name: "observedTimeUnixNano", value: record.Observed},
				} {
					if stamp.value == "" {
						continue
					}
					if err := validateStamp(stamp.value); err != nil {
						return fmt.Errorf("%s %s: %w", position, stamp.name, err)
					}
				}
			}
		}
	}
	for resourceIndex, resource := range exported.ResourceSpans {
		for scopeIndex, scope := range resource.ScopeSpans {
			for spanIndex, span := range scope.Spans {
				position := fmt.Sprintf(
					"line %d OTLP span %d/%d/%d", lineNumber, resourceIndex, scopeIndex, spanIndex,
				)
				for _, field := range []struct {
					name  string
					value string
				}{
					{name: "traceId", value: span.TraceID},
					{name: "spanId", value: span.SpanID},
					{name: "name", value: span.Name},
					{name: "startTimeUnixNano", value: span.Start},
					{name: "endTimeUnixNano", value: span.End},
				} {
					if field.value == "" {
						return fmt.Errorf("%s requires non-empty %s", position, field.name)
					}
				}
				if err := validateStamp(span.Start); err != nil {
					return fmt.Errorf("%s startTimeUnixNano: %w", position, err)
				}
				if err := validateStamp(span.End); err != nil {
					return fmt.Errorf("%s endTimeUnixNano: %w", position, err)
				}
			}
		}
	}
	return nil
}

// Discover loads provider manifests from user, environment, package, and XDG
// data directories. The first manifest for a name wins.
func Discover(settings Settings) (Registry, error) {
	directories := providerDirectories(settings)
	registry := Registry{}
	for _, directory := range directories {
		loaded, err := discoverDirectory(directory)
		if err != nil {
			return nil, err
		}
		for _, item := range loaded {
			if _, exists := registry[item.Manifest.Name]; !exists {
				registry[item.Manifest.Name] = item
			}
		}
	}
	return registry, nil
}

// discoverDirectory accepts both a flat manifest directory and the standard
// providers/<name>/provider.yaml layout. Flat files remain readable so an
// existing private provider does not break during migration.
func discoverDirectory(directory string) ([]sharedprovider.LoadedManifest, error) {
	loaded, err := sharedprovider.Discover(directory)
	if err != nil {
		return nil, err
	}
	for _, item := range loaded {
		if err := validateSupportedActions(item.Manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", item.Path, err)
		}
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return loaded, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read provider directory %s: %w", directory, err)
	}
	seen := make(map[string]string, len(loaded))
	for _, item := range loaded {
		seen[item.Manifest.Name] = item.Path
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		children, err := sharedprovider.Discover(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, item := range children {
			if err := validateSupportedActions(item.Manifest); err != nil {
				return nil, fmt.Errorf("%s: %w", item.Path, err)
			}
			if previous, exists := seen[item.Manifest.Name]; exists {
				return nil, fmt.Errorf("duplicate provider %q in %s and %s", item.Manifest.Name, previous, item.Path)
			}
			seen[item.Manifest.Name] = item.Path
			loaded = append(loaded, item)
		}
	}
	return loaded, nil
}

func providerDirectories(settings Settings) []string {
	var candidates []string
	if settings.Providers.Directory != "" {
		candidates = append(candidates, settings.Providers.Directory)
	}
	if value := strings.TrimSpace(os.Getenv("TRACES_PROVIDER_PATH")); value != "" {
		candidates = append(candidates, filepath.SplitList(value)...)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Clean(filepath.Join(filepath.Dir(executable), "share", "traces", "providers")),
			filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "share", "traces", "providers")),
		)
	}
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	if dataHome != "" {
		candidates = append(candidates, filepath.Join(dataHome, "traces", "providers"))
	}
	dataDirs := strings.TrimSpace(os.Getenv("XDG_DATA_DIRS"))
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, directory := range filepath.SplitList(dataDirs) {
		candidates = append(candidates, filepath.Join(directory, "traces", "providers"))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, directory := range candidates {
		directory = filepath.Clean(directory)
		if directory == "." || seen[directory] {
			continue
		}
		seen[directory] = true
		out = append(out, directory)
	}
	return out
}

// Names returns provider names in stable order.
func (registry Registry) Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (registry Registry) supporting(action string) []sharedprovider.LoadedManifest {
	loaded := make([]sharedprovider.LoadedManifest, 0, len(registry))
	for _, item := range registry {
		if _, ok := item.Manifest.Actions[action]; ok {
			loaded = append(loaded, item)
		}
	}
	sort.Slice(loaded, func(i, j int) bool {
		left, right := loaded[i].Manifest, loaded[j].Manifest
		if left.Defaults.Priority != right.Defaults.Priority {
			return left.Defaults.Priority > right.Defaults.Priority
		}
		return left.Name < right.Name
	})
	return loaded
}

// CurrentSession asks provider capabilities for the native session identity.
func (registry Registry) CurrentSession(ctx context.Context, directory string) string {
	for _, loaded := range registry.supporting(ActionSessionCurrent) {
		plan, err := loaded.Manifest.Render(ActionSessionCurrent, actionData{Directory: directory})
		if err != nil {
			continue
		}
		output, _, err := runPlan(ctx, plan, directory)
		if err == nil && strings.TrimSpace(string(output)) != "" {
			return strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
		}
	}
	return ""
}

// DiscoverSessions asks every capable provider for directory-bound session IDs.
func (registry Registry) DiscoverSessions(ctx context.Context, directory string) []string {
	seen := map[string]bool{}
	var sessions []string
	for _, loaded := range registry.supporting(ActionSessionDiscover) {
		plan, err := loaded.Manifest.Render(ActionSessionDiscover, actionData{Directory: directory})
		if err != nil {
			continue
		}
		output, _, err := runPlan(ctx, plan, directory)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(output), "\n") {
			id := strings.TrimSpace(line)
			if id != "" && !seen[id] {
				seen[id] = true
				sessions = append(sessions, id)
			}
		}
	}
	return sessions
}

// Resolve picks the sources to read. An explicit ask, from the flag or from the
// environment, is taken as given and never scoped: it is the escape hatch for a
// one-off read. With no ask, the per-harness table decides, and a source whose
// harness the reader filtered out is not fetched at all.
//
// The collector file is read either way. Only one harness on a machine usually
// needs a source beyond it.
func Resolve(ask, service string, settings Settings) ([]*Provider, error) {
	registry, err := Discover(settings)
	if err != nil {
		return nil, err
	}
	return ResolveRegistry(ask, service, settings, registry)
}

// ResolveRegistry selects activity providers from an already-discovered set.
func ResolveRegistry(ask, service string, settings Settings, registry Registry) ([]*Provider, error) {
	if ask == "" {
		ask = strings.TrimSpace(os.Getenv(Env))
	}
	if ask == "" {
		return declared(service, settings, registry)
	}
	out := []*Provider{}
	seen := map[string]bool{}
	for _, one := range strings.Split(ask, ",") {
		one = strings.TrimSpace(one)
		// A trailing comma, or a variable set to the empty string, is a list of
		// nothing rather than a provider named "".
		if one == "" || seen[one] {
			continue
		}
		seen[one] = true
		found, err := resolveOne(one, registry, ActionActivityRead)
		if err != nil {
			return nil, err
		}
		out = append(out, found)
	}
	return out, nil
}

// declared resolves the config table. A source the table names but the machine
// does not carry is dropped with a warning rather than failing the read: the
// table is shared configuration, and a harness that is not installed here is
// the normal case rather than an error.
func declared(service string, settings Settings, registry Registry) ([]*Provider, error) {
	out := []*Provider{}
	for _, one := range wanted(settings.Sources, service) {
		found, err := resolveOne(one.Name, registry, ActionActivityRead)
		if err != nil {
			fmt.Fprintf(os.Stderr, "traces: skipped %v\n", err)
			continue
		}
		found.harnesses = one.harnesses
		out = append(out, found)
	}
	return out, nil
}

func resolveOne(name string, registry Registry, action string) (*Provider, error) {
	loaded, configured := registry[name]
	if !configured {
		return nil, fmt.Errorf("provider %q has no discovered manifest", name)
	}
	if _, ok := loaded.Manifest.Actions[action]; !ok {
		return nil, fmt.Errorf("provider %q does not advertise %s", name, action)
	}
	found, err := exec.LookPath(loaded.Manifest.Command[0])
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", name, err)
	}
	loaded.Manifest.Command = append([]string{found}, loaded.Manifest.Command[1:]...)
	return &Provider{Manifest: loaded.Manifest, Path: loaded.Path, Name: name}, nil
}

// ResolveNamed returns one provider for a capability. An empty name disables it.
func ResolveNamed(name, action string, registry Registry) (*Provider, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	return resolveOne(strings.TrimSpace(name), registry, action)
}

// Fetch runs the provider once over the window ending now.
func (p Provider) Fetch(ctx context.Context, window time.Duration) (otlp.Batch, error) {
	plan, err := p.Manifest.Render(ActionActivityRead, actionData{
		Since: window.Round(time.Second).String(), Session: p.Session, Directory: p.Directory,
	})
	if err != nil {
		return otlp.Batch{}, fmt.Errorf("%s: %w", p.Name, err)
	}
	out, stderr, err := runPlan(ctx, plan, p.Directory)
	if err != nil {
		return otlp.Batch{}, fmt.Errorf("%s: %w: %s", p.Name, err, strings.TrimSpace(stderr))
	}
	return Decode(out), nil
}

// RenderDiff runs the provider's two-file rendering action.
func (p Provider) RenderDiff(
	ctx context.Context,
	local, remote, merged string,
	width int,
) (string, error) {
	color := strings.TrimSpace(p.Color)
	if color == "" {
		color = "auto"
	}
	plan, err := p.Manifest.Render(ActionDiffRender, actionData{
		Local: local, Remote: remote, Merged: merged, Width: width, Color: color,
	})
	if err != nil {
		return "", err
	}
	output, stderr, err := runPlan(ctx, plan, filepath.Dir(merged))
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", fmt.Errorf("%s: %w: %s", p.Name, err, strings.TrimSpace(stderr))
		}
	}
	return strings.TrimSpace(string(output)), nil
}

func runPlan(ctx context.Context, plan sharedprovider.Plan, directory string) ([]byte, string, error) {
	return runPlanWithEnvironment(ctx, plan, directory, os.Environ())
}

func runPlanWithEnvironment(
	ctx context.Context,
	plan sharedprovider.Plan,
	directory string,
	environment []string,
) ([]byte, string, error) {
	if len(plan.Argv) == 0 {
		return nil, "", fmt.Errorf("provider rendered an empty command")
	}
	if plan.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, plan.Timeout)
		defer cancel()
	}
	command := exec.CommandContext(ctx, plan.Argv[0], plan.Argv[1:]...)
	if directory != "" {
		command.Dir = directory
	}
	command.Env = mergeEnvironment(environment, plan.Env)
	stderr := &strings.Builder{}
	command.Stderr = stderr
	output, err := command.Output()
	return output, stderr.String(), err
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

// line is the shape a provider prints. It is deliberately flatter than the OTLP
// wire format, because a provider is often a shell script and the keyValue
// lists are what makes that painful to emit.
type line struct {
	// event marks the line as a log record rather than a span. A harness puts
	// on a log what it cannot put on a span, and the prompt is the one traces
	// needs.
	Event    string            `json:"event,omitempty"`
	TraceID  string            `json:"traceId,omitempty"`
	SpanID   string            `json:"spanId,omitempty"`
	ParentID string            `json:"parentId,omitempty"`
	Name     string            `json:"name,omitempty"`
	Service  string            `json:"service,omitempty"`
	Session  string            `json:"session,omitempty"`
	Start    string            `json:"startUnixNano,omitempty"`
	End      string            `json:"endUnixNano,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Failed   bool              `json:"failed,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// Encode writes a batch in the same shape a provider prints, so traces's own
// output is valid provider input. `traces --json | jq ... | traces` is the loop
// that makes traces a pipe stage rather than an application.
func Encode(w io.Writer, batch otlp.Batch) error {
	enc := json.NewEncoder(w)
	for _, one := range batch.Spans {
		if err := enc.Encode(line{
			TraceID:  one.TraceID,
			SpanID:   one.SpanID,
			ParentID: one.ParentID,
			Name:     one.Name,
			Service:  one.Service,
			Session:  one.Session,
			Start:    stampOf(one.Start),
			End:      stampOf(one.End),
			Attrs:    one.Attrs,
			Failed:   one.Failed,
			Error:    one.Error,
		}); err != nil {
			return err
		}
	}
	for _, one := range batch.Records {
		if err := enc.Encode(line{
			TraceID: one.TraceID,
			SpanID:  one.SpanID,
			Event:   one.Event,
			Name:    one.Body,
			Service: one.Service,
			Session: one.Session,
			Start:   stampOf(one.At),
			Attrs:   one.Attrs,
		}); err != nil {
			return err
		}
	}
	return nil
}

func stampOf(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return strconv.FormatInt(at.UnixNano(), 10)
}

// DecodeAny reads either shape. A line naming resourceSpans is one OTLP export
// request, which is what the collector's file holds; anything else is one flat
// span or record. Sniffing beats a flag: a reader piping a file in should not
// have to say which file it is.
func DecodeAny(blob []byte) otlp.Batch {
	out := otlp.Batch{}
	rows := bufio.NewScanner(bytes.NewReader(blob))
	rows.Buffer(make([]byte, 1<<20), 1<<26)
	for rows.Scan() {
		text := strings.TrimSpace(rows.Text())
		if text == "" || text[0] != '{' {
			continue
		}
		var one otlp.Batch
		if strings.Contains(text, `"resourceSpans"`) || strings.Contains(text, `"resourceLogs"`) {
			one = otlp.Decode([]byte(text))
		} else {
			one = Decode([]byte(text))
		}
		out.Spans = append(out.Spans, one.Spans...)
		out.Records = append(out.Records, one.Records...)
	}
	return out
}

func Decode(blob []byte) otlp.Batch {
	out := otlp.Batch{}
	rows := bufio.NewScanner(bytes.NewReader(blob))
	rows.Buffer(make([]byte, 1<<20), 1<<26)
	for rows.Scan() {
		text := strings.TrimSpace(rows.Text())
		if text == "" || text[0] != '{' {
			continue
		}
		var one line
		if json.Unmarshal([]byte(text), &one) != nil {
			continue
		}
		attrs := one.Attrs
		if attrs == nil {
			attrs = map[string]string{}
		}
		if one.Event != "" {
			out.Records = append(out.Records, otlp.Record{
				TraceID: one.TraceID,
				SpanID:  one.SpanID,
				Event:   one.Event,
				Body:    one.Name,
				Service: orAttr(one.Service, attrs, "service.name"),
				Session: sessionOf(one.Session, attrs),
				At:      otlp.Stamp(one.Start),
				Attrs:   attrs,
			})
			continue
		}
		if one.SpanID == "" {
			continue
		}
		span := otlp.Span{
			TraceID:  one.TraceID,
			SpanID:   one.SpanID,
			ParentID: one.ParentID,
			Name:     one.Name,
			Service:  orAttr(one.Service, attrs, "service.name"),
			Session:  sessionOf(one.Session, attrs),
			Start:    otlp.Stamp(one.Start),
			End:      otlp.Stamp(one.End),
			Attrs:    attrs,
			Failed:   one.Failed,
			Error:    one.Error,
		}
		if attrs["success"] == "false" || attrs["error"] != "" {
			span.Failed = true
			if span.Error == "" {
				span.Error = attrs["error"]
			}
		}
		out.Spans = append(out.Spans, span)
	}
	return out
}

func orAttr(given string, attrs map[string]string, key string) string {
	if given != "" {
		return given
	}
	return attrs[key]
}

func sessionOf(given string, attrs map[string]string) string {
	if given != "" {
		return given
	}
	return otlp.SessionID(attrs)
}

// Follow polls the provider and sends only the spans it has not sent before.
// The window overlaps every poll by lag, because a span is written when it ends
// and a queried source indexes it a moment later; the span id set is what keeps
// the overlap from arriving twice.
func Follow(p Provider, every, back, lag time.Duration, out chan<- otlp.Batch, stop <-chan struct{}) {
	defer close(out)

	seen := map[string]bool{}
	window := back
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		read, err := p.Fetch(ctx, window)
		cancel()

		if err == nil {
			batch := otlp.Batch{}
			for _, one := range read.Spans {
				if seen[one.SpanID] {
					continue
				}
				seen[one.SpanID] = true
				batch.Spans = append(batch.Spans, one)
			}
			// Join IDs separate tool results that share one millisecond.
			for _, one := range read.Records {
				joinID := first(one.SpanID, one.Attrs["request_id"], one.Attrs["tool_use_id"])
				key := one.Session + "/" + one.Event + "/" + joinID + "/" + one.At.Format(time.RFC3339Nano)
				if seen[key] {
					continue
				}
				seen[key] = true
				batch.Records = append(batch.Records, one)
			}
			if !batch.Empty() {
				select {
				case out <- batch:
				case <-stop:
					return
				}
			}
			// The first read covers the whole history the caller asked for.
			// Every read after it only has to cover the poll plus the lag.
			window = every + lag
		}

		select {
		case <-stop:
			return
		case <-time.After(every):
		}
	}
}

func first(parts ...string) string {
	for _, one := range parts {
		if one != "" {
			return one
		}
	}
	return ""
}
