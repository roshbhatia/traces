package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/go-utils/completion"
	"github.com/roshbhatia/traces/internal/otlp"
)

func TestRenderProviderList(t *testing.T) {
	got, err := renderProviderList(providerListView{
		Name: "local", Description: "Read local activity", Source: "/providers/local.yaml",
		Provides: "activity.read, session.current", Command: "local-provider --json",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `local
  Read local activity
  source    /providers/local.yaml
  provides  activity.read, session.current
  command   local-provider --json
`
	if got != want {
		t.Fatalf("provider list:\n%s\nwant:\n%s", got, want)
	}
}

func TestProviderColorResolvesAutoBeforeCapturedProviderOutput(t *testing.T) {
	previous := stdoutIsTerminal
	t.Cleanup(func() { stdoutIsTerminal = previous })
	stdoutIsTerminal = func() bool { return true }
	if got := providerColor("auto"); got != "always" {
		t.Fatalf("terminal auto color = %q, want always", got)
	}
	stdoutIsTerminal = func() bool { return false }
	if got := providerColor("auto"); got != "never" {
		t.Fatalf("piped auto color = %q, want never", got)
	}
	if got := providerColor("always"); got != "always" {
		t.Fatalf("explicit color = %q, want always", got)
	}
}

func TestGeneratedCompletionsPreserveProviderContext(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		want   []string
		reject []string
	}{
		"bash": {
			want: []string{
				"':provider') context='provider'",
				"printf '%s\\n' 'list' 'validate'",
				"':--provider') __traces_completion_filter \"$current\" < <(__traces_completion_values_1)",
				"__traces_completion_values_2",
				"__traces_completion_values_3",
				"'traces' 'provider' 'complete' \"${COMP_LINE:0:COMP_POINT}\"",
			},
			reject: []string{
				"completion generate provider list validate",
				"COMPREPLY=($(compgen",
				"candidates=\"$candidates",
			},
		},
		"fish": {
			want: []string{
				`test (__traces_completion_context) = "provider"' -a list`,
				`test (__traces_completion_context) = "provider"' -a validate`,
				`-l provider -r -a '(__traces_completion_values_1)'`,
				`= "provider list"' -a '(__traces_completion_values_2)'`,
				`= "provider validate"' -a '(__traces_completion_values_3)'`,
				"command 'traces' 'provider' 'complete' (commandline -cp)",
			},
			reject: []string{`test (__traces_completion_context) = ""' -a list`},
		},
		"nu": {
			want: []string{
				`export extern "traces provider list"`,
				`export extern "traces provider validate"`,
				`--provider: string@"__traces_completion_values_1"`,
				`...args: string@"__traces_completion_values_2"`,
				`...args: string@"__traces_completion_values_3"`,
				`run-external "traces" "provider" "complete" ($context | default "")`,
			},
			reject: []string{`export extern "traces list"`, `export extern "traces validate"`},
		},
		"zsh": {
			want: []string{
				"'1:command:(completion generate provider)'",
				"'2:command:(list validate)'",
				":value:__traces_completion_values_1",
				"'*:argument:__traces_completion_values_2'",
				"'*:argument:__traces_completion_values_3'",
				`'traces' 'provider' 'complete' "${BUFFER[1,CURSOR]}"`,
			},
			reject: []string{"'1:command:(completion generate provider list validate)'"},
		},
	}
	for shell, test := range tests {
		shell := shell
		test := test
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			generated, err := completion.Generate(shell, commandMetadata())
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !bytes.Contains([]byte(generated), []byte(want)) {
					t.Fatalf("completion lacks %q:\n%s", want, generated)
				}
			}
			for _, reject := range test.reject {
				if bytes.Contains([]byte(generated), []byte(reject)) {
					t.Fatalf("completion flattened provider command as %q:\n%s", reject, generated)
				}
			}
		})
	}
}

func TestProviderCompletionCandidatesPreserveCommaSeparatedSelection(t *testing.T) {
	names := []string{"claude", "codex", "git", "opencode"}
	tests := map[string][]string{
		"traces --provider ":              {"claude", "codex", "git", "opencode"},
		"traces --provider c":             {"claude", "codex"},
		"traces --provider claude,c":      {"claude,codex"},
		"traces --provider=claude,g":      {"claude,git"},
		"traces --provider claude,codex,": {"claude,codex,git", "claude,codex,opencode"},
		"traces provider validate op":     {"opencode"},
	}
	for context, want := range tests {
		if got := providerCompletionCandidates(context, names); !slices.Equal(got, want) {
			t.Errorf("candidates for %q = %#v, want %#v", context, got, want)
		}
	}
}

func TestBashCompletionTreatsProviderNamesAsLiteralText(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "unexpected-side-effect")
	providerName := "$(touch " + marker + ")"
	providerCommand := filepath.Join(directory, "traces")
	if err := os.WriteFile(providerCommand, []byte(`#!/bin/sh
printf '%s\n' 'provider with space' "$TRACES_COMPLETION_FIXTURE"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	generated, err := completion.Generate("bash", commandMetadata())
	if err != nil {
		t.Fatal(err)
	}
	completionPath := filepath.Join(directory, "traces.bash")
	if err := os.WriteFile(completionPath, []byte(generated), 0o600); err != nil {
		t.Fatal(err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(bash, "-c", `
complete() { :; }
source "$1"
COMP_WORDS=(traces --provider "")
COMP_CWORD=2
_traces_complete
printf '%s\0' "${COMPREPLY[@]}"
`, "completion-test", completionPath)
	command.Env = []string{
		"HOME=" + directory,
		"PATH=" + directory + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TRACES_COMPLETION_FIXTURE=" + providerName,
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Bash completion: %v\n%s", err, output)
	}
	got := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	want := []string{"provider with space", providerName}
	if !slices.Equal(got, want) {
		t.Fatalf("provider candidates = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Bash evaluated provider name as shell syntax: %v", err)
	}
}

func TestProviderArgumentsAcceptFlagsAroundName(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "example"},
		{"example", "--json"},
		{"--config", "config.yaml", "example", "--json"},
	} {
		flags, name, err := providerArguments(args)
		if err != nil {
			t.Fatal(err)
		}
		if name != "example" || !slices.Contains(flags, "--json") {
			t.Fatalf("args %v produced flags %v and name %q", args, flags, name)
		}
	}
}

func TestProviderValidateFailsOnDiscoveryIssues(t *testing.T) {
	if os.Getenv("TRACES_TEST_PROVIDER_VALIDATE") == "1" {
		runProvider([]string{"validate", "--json"})
		return
	}
	root := t.TempDir()
	providers := filepath.Join(root, "providers")
	if err := os.MkdirAll(providers, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providers, "broken.yaml"), []byte("version: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestProviderValidateFailsOnDiscoveryIssues$")
	command.Env = []string{
		"HOME=" + filepath.Join(root, "home"),
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
		"TRACES_PROVIDER_PATH=" + providers,
		"TRACES_TEST_PROVIDER_VALIDATE=1",
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_DATA_DIRS=" + filepath.Join(root, "data-dirs"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"),
	}
	output, err := command.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("provider validate: %v\n%s", err, output)
	}
	for _, want := range []string{"invalid provider manifest", "broken.yaml", "[]"} {
		if !bytes.Contains(output, []byte(want)) {
			t.Fatalf("provider validate output lacks %q:\n%s", want, output)
		}
	}
}

func TestSelectedJSONKeepsJoinedRuntimeFacets(t *testing.T) {
	now := time.Unix(1, 0)
	batch := otlp.Batch{Spans: []otlp.Span{
		{
			SpanID: "turn", Name: "agent.turn", Service: "example-agent", Session: "run-123",
			Start: now, End: now, Attrs: map[string]string{"traces.view": "activity"},
		},
		{
			SpanID: "tool", ParentID: "turn", Name: "agent.tool", Service: "example-agent", Session: "run-123",
			Start: now, End: now,
			Attrs: map[string]string{"traces.view": "activity", "tool_use_id": "call-1"},
		},
		{
			SpanID: "runtime", Name: "runtime.tool", Service: "example-agent", Session: "run-123",
			Start: now, End: now, Failed: true, Attrs: map[string]string{"tool_use_id": "call-1"},
		},
	}}

	selected := only(batch, "run-123", nil, "")
	if len(selected.Spans) != 3 {
		t.Fatalf("selected spans = %+v", selected.Spans)
	}
}

func TestFileInputSessionAliasesCanBeSelected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "flat",
			body: compactJSON(t, `{
				"traceId": "flat-trace",
				"spanId": "flat-root",
				"name": "runtime.interaction",
				"startUnixNano": "1788278400000000000",
				"endUnixNano": "1788278401000000000",
				"attrs": {"thread_id": "run-123"}
			}`),
		},
		{
			name: "otlp",
			body: compactJSON(t, `{
				"resourceSpans": [{
					"resource": {"attributes": [{
						"key": "conversation.id",
						"value": {"stringValue": "run-123"}
					}]},
					"scopeSpans": [{"spans": [{
						"traceId": "otlp-trace",
						"spanId": "otlp-root",
						"name": "runtime.interaction",
						"startTimeUnixNano": "1788278400000000000",
						"endTimeUnixNano": "1788278401000000000"
					}]}]
				}]
			}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "activity.jsonl")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			batch, err := (sources{path: path}).read()
			if err != nil {
				t.Fatal(err)
			}
			selected := only(batch, "run-123", nil, "")
			if len(selected.Spans) != 1 || selected.Spans[0].Session != "run-123" {
				t.Fatalf("selected batch = %+v", selected)
			}
		})
	}
}

func compactJSON(t *testing.T, value string) string {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(value)); err != nil {
		t.Fatal(err)
	}
	return compact.String()
}
