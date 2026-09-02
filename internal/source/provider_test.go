package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, settings := writeSettings(t, "color: auto\nproviders:\n  local:\n    command: ["+executable+"]\n    capabilities: [activity]\nsources:\n  codex: [local]\ndiff:\n  command: []\n")
	t.Setenv("TRACES_COLOR", "never")
	path := filepath.Join(t.TempDir(), "override.yaml")
	if err := os.WriteFile(path, []byte("color: always\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadSettings(path)
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
	if len(settings.Providers) != 0 || len(settings.Sources) != 0 {
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
	settings.Providers["local"] = Manifest{
		Command: []string{executable, "-test.run=none"}, Capabilities: []string{"activity"},
	}
	settings.Sources["codex"] = []string{"local"}
	providers, err := Resolve("", "codex", settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Command[0] != executable || providers[0].Command[1] != "-test.run=none" {
		t.Fatalf("providers = %+v", providers)
	}
}

func TestResolveRejectsWrongCapability(t *testing.T) {
	settings := Default()
	settings.Providers["display"] = Manifest{Command: []string{os.Args[0]}, Capabilities: []string{"display"}}
	_, err := Resolve("display", "", settings)
	if err == nil || !strings.Contains(err.Error(), "does not advertise activity") {
		t.Fatalf("error = %v", err)
	}
}

func TestSchemaDescribesProviderCommands(t *testing.T) {
	data, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"$schema"`, `"providers"`, `"capabilities"`, `"diff"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("schema omits %s", want)
		}
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
