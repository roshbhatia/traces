package source

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	_, settings := writeSettings(t, "color: auto\nproviders:\n  directory: /tmp/providers\nsources:\n  codex: [local]\ndiff:\n  provider: git\n")
	t.Setenv("TRACES_COLOR", "never")
	path := filepath.Join(t.TempDir(), "override.yaml")
	if err := os.WriteFile(path, []byte("color: always\n"), 0o600); err != nil {
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
		"claude-code": {"observe", "claude"},
		"codex":       {"codex"},
	}
	got := wanted(table, "codex")
	if len(got) != 1 || got[0].Name != "codex" {
		t.Fatalf("providers = %v", got)
	}
}

func TestOneSourceServingTwoHarnessesIsFetchedOnce(t *testing.T) {
	got := wanted(map[string][]string{
		"claude-code": {"observe"},
		"codex":       {"observe"},
	}, "")
	if len(got) != 1 || got[0].For() != "claude-code+codex" {
		t.Fatalf("providers = %v", got)
	}
}

func TestResolveUsesManifestCommand(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	settings := Default()
	settings.Sources["codex"] = []string{"local"}
	registry := Registry{"local": {Manifest: testManifest("local", []string{executable, "-test.run=none"}, ActionActivityRead)}}
	providers, err := ResolveRegistry("", "codex", settings, registry)
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
	settings.Sources["codex"] = []string{"not-installed"}
	providers, err := ResolveRegistry("", "codex", settings, Registry{})
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
		"first":  {Manifest: commandManifest("first", shell, ActionSessionDiscover, `printf 'one\ntwo\n'`)},
		"second": {Manifest: commandManifest("second", shell, ActionSessionDiscover, `printf 'two\nthree\n'`)},
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
			`test "$1" = "2m0s" && test "$2" = "session with spaces" && test -d "$3" && test "$EXPECTED_SESSION" = "$2" && printf '%s\n' '{"traceId":"demo","spanId":"root","name":"rendered"}'`,
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
	manifest := testManifest("local", []string{shell, "-c", `printf '%s\n' '{"traceId":"demo","spanId":"root","name":"validation"}'`}, ActionActivityRead)
	result := Validate(context.Background(), "local", sharedprovider.LoadedManifest{Manifest: manifest}, t.TempDir())
	if result.Status != "ok" {
		t.Fatalf("validation = %+v", result)
	}
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
	body := fmt.Sprintf(`version: provider/v1
name: %s
description: %s
command: [sh]
actions:
  activity.read:
    description: read activity
`, name, description)
	if err := os.WriteFile(filepath.Join(directory, filename), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeFlatProviderSpan(t *testing.T) {
	batch := DecodeAny([]byte(`{"traceId":"demo","spanId":"root","name":"Rewrite the renderer","service":"codex","session":"demo","startUnixNano":"1788278400000000000","endUnixNano":"1788278408000000000"}`))
	if len(batch.Spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(batch.Spans))
	}
	if batch.Spans[0].Session != "demo" || batch.Spans[0].Service != "codex" {
		t.Fatalf("span = %#v", batch.Spans[0])
	}
}
