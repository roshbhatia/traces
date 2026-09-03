package source

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	sharedprovider "github.com/roshbhatia/go-utils/provider"
)

func writeSettings(t *testing.T, body string) (string, Settings) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, settings
}

func TestLoadSettingsAndEnvironment(t *testing.T) {
	_, settings := writeSettings(t, `color: auto
providers:
  directory: /tmp/providers
sources:
  runner-a: [local]
diff:
  provider: local
`)
	t.Setenv("TRACES_COLOR", "never")
	path := filepath.Join(t.TempDir(), "override.yaml")
	if err := os.WriteFile(path, []byte(`color: always
`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Color != "never" {
		t.Fatalf("color = %q", settings.Color)
	}
}

func TestCoreDefaultsHaveNoHarnessProviders(t *testing.T) {
	settings, err := LoadSettings(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if settings.Diff.Provider != "" || len(settings.Sources) != 0 {
		t.Fatalf("defaults include integrations: %+v", settings)
	}
}

func TestServiceFilterSkipsOtherHarnesses(t *testing.T) {
	table := map[string][]string{
		"runner-a": {"shared", "local"},
		"runner-b": {"remote"},
	}
	got := wanted(table, "runner-b")
	if len(got) != 1 || got[0].Name != "remote" {
		t.Fatalf("providers = %v", got)
	}
}

func TestOneSourceServingTwoHarnessesIsFetchedOnce(t *testing.T) {
	got := wanted(map[string][]string{
		"runner-a": {"shared"},
		"runner-b": {"shared"},
	}, "")
	if len(got) != 1 || got[0].For() != "runner-a+runner-b" {
		t.Fatalf("providers = %v", got)
	}
}

func TestResolveUsesManifestCommand(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	settings := Default()
	settings.Sources["runner-a"] = []string{"local"}
	registry := Registry{"local": {Manifest: testManifest("local", []string{executable, "-test.run=none"}, ActionActivityRead)}}
	providers, err := ResolveRegistry("", "runner-a", settings, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Manifest.Command[0] != executable || providers[0].Manifest.Command[1] != "-test.run=none" {
		t.Fatalf("providers = %+v", providers)
	}
}

func TestResolveRejectsWrongCapability(t *testing.T) {
	settings := Default()
	registry := Registry{"display": {Manifest: testManifest("display", []string{os.Args[0]}, "display.open")}}
	_, err := ResolveRegistry("display", "", settings, registry)
	if err == nil || !strings.Contains(err.Error(), "does not advertise activity.read") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfiguredMissingProviderFailsOpen(t *testing.T) {
	settings := Default()
	settings.Sources["runner-a"] = []string{"not-installed"}
	providers, err := ResolveRegistry("", "runner-a", settings, Registry{})
	if err != nil || len(providers) != 0 {
		t.Fatalf("providers = %v, error = %v", providers, err)
	}
}

func TestExplicitMissingProviderFails(t *testing.T) {
	_, err := ResolveRegistry("not-installed", "", Default(), Registry{})
	if err == nil || !strings.Contains(err.Error(), "no discovered manifest") {
		t.Fatalf("error = %v", err)
	}
}

func TestDiscoverUsesConfiguredDirectoryBeforeEnvironment(t *testing.T) {
	configured := t.TempDir()
	environment := t.TempDir()
	configuredProvider := filepath.Join(configured, "shared")
	if err := os.Mkdir(configuredProvider, 0o700); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, configuredProvider, "provider.yaml", "shared", "configured")
	writeManifest(t, environment, "shared.yaml", "shared", "environment")
	writeManifest(t, environment, "extra.yaml", "extra", "environment")
	t.Setenv("TRACES_PROVIDER_PATH", environment)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_DATA_DIRS", t.TempDir())

	settings := Default()
	settings.Providers.Directory = configured
	registry, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	if got := registry["shared"].Manifest.Description; got != "configured" {
		t.Fatalf("shared description = %q", got)
	}
	if _, ok := registry["extra"]; !ok {
		t.Fatal("environment provider was not discovered")
	}
}

func TestCurrentSessionUsesHighestPriorityCapability(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	low := commandManifest("low", shell, ActionSessionCurrent, `printf 'low\n'`)
	high := commandManifest("high", shell, ActionSessionCurrent, `printf 'high\n'`)
	high.Defaults.Priority = 10
	registry := Registry{
		"low":  {Manifest: low},
		"high": {Manifest: high},
	}
	if got := registry.CurrentSession(context.Background(), t.TempDir()); got != "high" {
		t.Fatalf("current session = %q", got)
	}
}

func TestDiscoverSessionsMergesCapabilities(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	registry := Registry{
		"first": {Manifest: commandManifest("first", shell, ActionSessionDiscover, `cat <<'SESSIONS'
one
two
SESSIONS
`)},
		"second": {Manifest: commandManifest("second", shell, ActionSessionDiscover, `cat <<'SESSIONS'
two
three
SESSIONS
`)},
	}
	got := registry.DiscoverSessions(context.Background(), t.TempDir())
	if strings.Join(got, ",") != "one,two,three" {
		t.Fatalf("sessions = %v", got)
	}
}

func TestFetchRendersArgumentsAndEnvironmentWithoutAWrapper(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	manifest := testManifest("render", []string{shell, "-c"}, ActionActivityRead)
	manifest.Actions[ActionActivityRead] = sharedprovider.Action{
		Description: "read activity",
		Argv: []string{
			`test "$1" = "2m0s"
test "$2" = "session with spaces"
test -d "$3"
test "$EXPECTED_SESSION" = "$2"
cat <<'JSON'
{"traceId":"demo","spanId":"root","name":"rendered"}
JSON
`,
			"provider", "{{ .Since }}", "{{ .Session }}", "{{ .Directory }}",
		},
		Env: map[string]string{"EXPECTED_SESSION": "{{ .Session }}"},
	}
	provider := Provider{Manifest: manifest, Name: "render", Session: "session with spaces", Directory: directory}
	batch, err := provider.Fetch(context.Background(), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Spans) != 1 || batch.Spans[0].Name != "rendered" {
		t.Fatalf("batch = %+v", batch)
	}
}

func TestSchemaDescribesProviderCommands(t *testing.T) {
	data, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"$schema"`, `"providers"`, `"directory"`, `"diff"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("schema omits %s", want)
		}
	}
}

func TestValidateRunsProviderAndChecksProtocol(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	script := `cat <<'JSON'
{"traceId":"demo","spanId":"root","name":"validation","startUnixNano":"1","endUnixNano":"2"}
JSON
`
	manifest := testManifest("local", []string{shell, "-c", script}, ActionActivityRead)
	result := Validate(context.Background(), "local", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir())
	if result.Status != "ok" {
		t.Fatalf("validation = %+v", result)
	}
}

func TestValidateExecutesEveryStandardActionInAnIsolatedEnvironment(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PROVIDER_TEST_SESSION", "live-session")
	t.Setenv("PROVIDER_TEST_UNDECLARED", "private")
	manifest := testManifest(
		"complete",
		[]string{shell, "-c"},
		ActionActivityRead,
		ActionDiffRender,
		ActionSessionCurrent,
		ActionSessionDiscover,
	)
	manifest.Requires.Environment = []string{"PROVIDER_TEST_SESSION"}
	manifest.Actions[ActionActivityRead] = sharedprovider.Action{
		Description: "read activity",
		Argv: []string{`printf 'activity.read\n' >> "$MARKER"
cat <<'JSON'
{"traceId":"demo","spanId":"root","name":"validation","startUnixNano":"1","endUnixNano":"2"}
JSON
`},
		Env: map[string]string{"MARKER": marker},
	}
	manifest.Actions[ActionDiffRender] = sharedprovider.Action{
		Description: "render diff",
		Argv: []string{`printf 'diff.render\n' >> "$MARKER"
printf 'diff output\n'
`},
		Env: map[string]string{"MARKER": marker},
	}
	manifest.Actions[ActionSessionCurrent] = sharedprovider.Action{
		Description: "read current session",
		Argv: []string{`test "${PROVIDER_TEST_SESSION:-}" = "live-session"
test -z "${PROVIDER_TEST_UNDECLARED:-}"
printf 'session.current\n' >> "$MARKER"
printf 'current-session\n'
`},
		Env: map[string]string{"MARKER": marker},
	}
	manifest.Actions[ActionSessionDiscover] = sharedprovider.Action{
		Description: "discover sessions",
		Argv: []string{`printf 'session.discover\n' >> "$MARKER"
printf 'session-one\nsession-two\n'
`},
		Env: map[string]string{"MARKER": marker},
	}

	result := Validate(context.Background(), "complete", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir())
	if result.Status != "ok" {
		t.Fatalf("validation = %+v", result)
	}
	blob, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	want := `activity.read
diff.render
session.current
session.discover
`
	if string(blob) != want {
		t.Fatalf("actions:\n%s\nwant:\n%s", blob, want)
	}
}

func TestValidateFailsWhenSessionActionCannotRun(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionSessionCurrent, ActionSessionDiscover} {
		t.Run(action, func(t *testing.T) {
			manifest := commandManifest("broken-session", shell, action, `exit 9`)
			result := Validate(
				context.Background(), "broken-session", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir(),
			)
			if result.Status != "failed" {
				t.Fatalf("validation = %+v", result)
			}
			found := false
			for _, check := range result.Checks {
				if check.Name == "probe:"+action && check.Status == "failed" {
					found = true
				}
			}
			if !found {
				t.Fatalf("validation omitted failed session probe: %+v", result)
			}
		})
	}
}

func TestValidateRejectsMultipleCurrentSessions(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	manifest := commandManifest(
		"ambiguous-session", shell, ActionSessionCurrent, `printf 'session-one\nsession-two\n'`,
	)
	result := Validate(
		context.Background(), "ambiguous-session", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir(),
	)
	if result.Status != "failed" {
		t.Fatalf("validation = %+v", result)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name == "probe:"+ActionSessionCurrent && strings.Contains(check.Message, "more than one") {
			found = true
		}
	}
	if !found {
		t.Fatalf("validation accepted ambiguous current session: %+v", result)
	}
}

func TestValidateRejectsMalformedSpanFields(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "missing trace id",
			line: `{"spanId":"root","name":"validation","startUnixNano":"1","endUnixNano":"2"}`,
			want: "requires non-empty traceId",
		},
		{
			name: "missing name",
			line: `{"traceId":"demo","spanId":"root","startUnixNano":"1","endUnixNano":"2"}`,
			want: "requires non-empty name",
		},
		{
			name: "missing end stamp",
			line: `{"traceId":"demo","spanId":"root","name":"validation","startUnixNano":"1"}`,
			want: "requires non-empty endUnixNano",
		},
		{
			name: "numeric span id",
			line: `{"traceId":"demo","spanId":7,"name":"validation","startUnixNano":"1","endUnixNano":"2"}`,
			want: "invalid span field type",
		},
		{
			name: "non-string attribute",
			line: `{"traceId":"demo","spanId":"root","name":"validation","startUnixNano":"1","endUnixNano":"2","attrs":{"attempt":2}}`,
			want: "invalid span field type",
		},
		{
			name: "invalid stamp",
			line: `{"traceId":"demo","spanId":"root","name":"validation","startUnixNano":"soon","endUnixNano":"2"}`,
			want: "must be a decimal nanosecond string",
		},
		{
			name: "invalid OTLP array",
			line: `{"resourceSpans":{}}`,
			want: "resourceSpans must be an array",
		},
		{
			name: "missing OTLP name",
			line: `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"demo","spanId":"root","startTimeUnixNano":"1","endTimeUnixNano":"2"}]}]}]}`,
			want: "requires non-empty name",
		},
		{
			name: "invalid OTLP resource attribute",
			line: `{
  "resourceSpans": [{
    "resource": {"attributes": [{"key": "attempt", "value": {"intValue": 2}}]},
    "scopeSpans": []
  }]
}`,
			want: "invalid OTLP field type",
		},
		{
			name: "invalid OTLP span attribute",
			line: `{
  "resourceSpans": [{"scopeSpans": [{"spans": [{
    "traceId": "demo",
    "spanId": "root",
    "name": "work",
    "startTimeUnixNano": "1",
    "endTimeUnixNano": "2",
    "attributes": [{"key": "attempt", "value": {"intValue": 2}}]
  }]}]}]
}`,
			want: "invalid OTLP field type",
		},
		{
			name: "invalid OTLP log timestamp type",
			line: `{
  "resourceLogs": [{"scopeLogs": [{"logRecords": [{"timeUnixNano": 2}]}]}]
}`,
			want: "invalid OTLP field type",
		},
		{
			name: "missing OTLP log timestamp",
			line: `{
  "resourceLogs": [{"scopeLogs": [{"logRecords": [{"eventName": "message"}]}]}]
}`,
			want: "requires timeUnixNano or observedTimeUnixNano",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := testManifest(
				"invalid-fields",
				[]string{shell, "-c", `printf '%s\n' "$1"`, "provider", compactJSON(t, test.line)},
				ActionActivityRead,
			)
			result := Validate(
				context.Background(), "invalid-fields", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir(),
			)
			if result.Status != "failed" || !strings.Contains(result.Checks[len(result.Checks)-1].Message, test.want) {
				t.Fatalf("validation = %+v, want %q", result, test.want)
			}
		})
	}
}

func compactJSON(t *testing.T, source string) string {
	t.Helper()
	var output bytes.Buffer
	if err := json.Compact(&output, []byte(source)); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestValidateRejectsInvalidProtocol(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifest("invalid", []string{shell, "-c", `printf 'not json\n'`}, ActionActivityRead)
	result := Validate(context.Background(), "invalid", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir())
	if result.Status != "failed" || !strings.Contains(result.Checks[len(result.Checks)-1].Message, "not a JSON object") {
		t.Fatalf("validation = %+v", result)
	}
}

func TestValidateRejectsUnsupportedAction(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	manifest := commandManifest("unsupported", shell, "activity.typo", `exit 0`)
	result := Validate(
		context.Background(), "unsupported", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir(),
	)
	if result.Status != "failed" {
		t.Fatalf("validation = %+v", result)
	}
	last := result.Checks[len(result.Checks)-1]
	if last.Name != "manifest:actions" || !strings.Contains(last.Message, "unsupported action") {
		t.Fatalf("unsupported action check = %+v", last)
	}
}

func TestDiscoverRejectsUnsupportedAction(t *testing.T) {
	directory := t.TempDir()
	manifest := `version: provider/v1
name: unsupported
description: Invalid capability
command: [sh]
actions:
  activity.typo:
    description: Misspelled activity action
`
	if err := os.WriteFile(filepath.Join(directory, "provider.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverDirectory(directory); err == nil || !strings.Contains(err.Error(), "unsupported action") {
		t.Fatalf("discover error = %v", err)
	}
}

func TestProviderSchemaRestrictsActionNames(t *testing.T) {
	data, err := ProviderSchema()
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			PropertyNames struct {
				Enum []string `json:"enum"`
			} `json:"propertyNames"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	got := schema.Properties["actions"].PropertyNames.Enum
	if strings.Join(got, ",") != strings.Join(supportedActionNames, ",") {
		t.Fatalf("action schema = %v, want %v", got, supportedActionNames)
	}
}

func testManifest(name string, command []string, actions ...string) sharedprovider.Manifest {
	manifest := sharedprovider.Manifest{
		Version: sharedprovider.Version, Name: name, Description: name + " provider",
		Command: command, Actions: map[string]sharedprovider.Action{},
	}
	for _, action := range actions {
		manifest.Actions[action] = sharedprovider.Action{Description: action}
	}
	return manifest
}

func commandManifest(name, shell, capability, script string) sharedprovider.Manifest {
	manifest := testManifest(name, []string{shell, "-c"}, capability)
	manifest.Actions[capability] = sharedprovider.Action{
		Description: capability,
		Argv:        []string{script},
	}
	return manifest
}

func writeManifest(t *testing.T, directory, filename, name, description string) {
	t.Helper()
	body := renderTestTemplate(t, "provider manifest", `version: provider/v1
name: {{ .Name }}
description: {{ .Description }}
command: [sh]
actions:
  activity.read:
    description: read activity
`, struct {
		Name        string
		Description string
	}{Name: name, Description: description})
	if err := os.WriteFile(filepath.Join(directory, filename), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func renderTestTemplate(t *testing.T, name, source string, data any) string {
	t.Helper()
	parsed, err := template.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := parsed.Execute(&output, data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestDecodeFlatProviderSpan(t *testing.T) {
	batch := DecodeAny([]byte(`{"traceId":"demo","spanId":"root","name":"Rewrite the renderer","service":"example-agent","session":"demo","startUnixNano":"1788278400000000000","endUnixNano":"1788278408000000000"}`))
	if len(batch.Spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(batch.Spans))
	}
	if batch.Spans[0].Session != "demo" || batch.Spans[0].Service != "example-agent" {
		t.Fatalf("span = %#v", batch.Spans[0])
	}
}
