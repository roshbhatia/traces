package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"

	sharedprovider "github.com/roshbhatia/go-utils/provider"
	"github.com/roshbhatia/traces/internal/otlp"
	"github.com/roshbhatia/traces/internal/session"
	"github.com/roshbhatia/traces/internal/source"
)

func TestChangesTabRendersEditOutput(t *testing.T) {
	isolateDiffCache(t)
	patch := `## internal/file.go

@@ -1 +1 @@
-old
+new
`
	model := Model{pane: viewport.New(100, 20)}
	out := model.tabChanges(row{label: "Edit", node: &session.Node{Output: patch}})
	if !strings.Contains(out, "file.go") || !strings.Contains(out, "+1") || !strings.Contains(out, "-1") {
		t.Fatalf("changes tab = %q", out)
	}
}

func TestDiffRendererFallsBackWithoutProvider(t *testing.T) {
	isolateDiffCache(t)
	patch := `--- a/file.go
+++ b/file.go
@@ -1 +1 @@
-old
+new
`
	out := newDiffRenderer().render(patch, 80)
	if !strings.Contains(out, "old") || !strings.Contains(out, "new") {
		t.Fatalf("fallback output:\n%s", out)
	}
}

func TestDiffProviderCachesByContentAndWidth(t *testing.T) {
	isolateDiffCache(t)
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	provider := filepath.Join(dir, "provider")
	script := renderTestTemplate(t, "diff provider", `#!/bin/sh
printf x >> "{{ .Counter }}"
printf 'provider view\n'
`, struct{ Counter string }{Counter: counter})
	if err := os.WriteFile(provider, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	renderer := newDiffRenderer(diffProvider("test", []string{provider}, nil))
	patch := `--- a/file.go
+++ b/file.go
@@ -1 +1 @@
-old
+new
`
	if first, second := renderer.render(patch, 80), renderer.render(patch, 80); first != second || first != "provider view" {
		t.Fatalf("renders = %q and %q", first, second)
	}
	calls, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "x" {
		t.Fatalf("provider calls = %q, want one", calls)
	}
}

func TestDiffProviderReceivesTwoFileArguments(t *testing.T) {
	isolateDiffCache(t)
	directory := t.TempDir()
	provider := filepath.Join(directory, "provider")
	script := `#!/bin/sh
set -eu
test -f "$1"
test -f "$2"
test "$3" = "file.go"
test "$4" = "80"
test "$5" = "never"
printf -- '-'
cat "$1"
printf -- '+'
cat "$2"
`
	if err := os.WriteFile(provider, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	patch := `--- a/file.go
+++ b/file.go
@@ -1 +1 @@
-old
+new
`
	configured := diffProvider("test", []string{provider}, []string{
		"{{ .Local }}", "{{ .Remote }}", "{{ .Merged }}", "{{ .Width }}", "{{ .Color }}",
	})
	configured.Color = "never"
	renderer := newDiffRenderer(configured)
	out := renderer.render(patch, 80)
	if !strings.Contains(out, "-old") || !strings.Contains(out, "+new") {
		t.Fatalf("provider output:\n%s", out)
	}
}

func diffProvider(name string, command, argv []string) *source.Provider {
	return &source.Provider{Name: name, Manifest: sharedprovider.Manifest{
		Version: sharedprovider.Version,
		Name:    name, Description: name + " diff", Command: command,
		Actions: map[string]sharedprovider.Action{
			source.ActionDiffRender: {Description: "render diff", Argv: argv},
		},
	}}
}

func TestInspectorDetectsStructuredOutput(t *testing.T) {
	for name, input := range map[string]string{
		"json": `{"ok":true,"count":2}`,
		"json lines": `{"ok":true}
{"ok":false}`,
		"diff": `--- old
+++ new
@@ -1 +1 @@
-old
+new`,
	} {
		if got := detectSyntax(input, ""); got == "" {
			t.Errorf("%s syntax was not detected", name)
		}
	}
	if got := detectSyntax("\x1b[31mred\x1b[0m", "bash"); got != "" {
		t.Errorf("ANSI syntax = %q, want empty", got)
	}
	if got := detectSyntax("printf '%s\\n' ready", "bash"); got != "bash" {
		t.Errorf("shell syntax = %q, want bash", got)
	}
}

func TestInspectorColorsJSONWithTerminalPalette(t *testing.T) {
	m := Model{width: 100, height: 40, split: 50, place: placeBottom, pane: viewport.New(80, 20)}.sized()
	lines := m.codeLines(`{"ok":true,"count":2}`, "json", 80)
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("colored JSON = %q", out)
	}
	if plain := strings.TrimSpace(ansi.Strip(out)); plain != `{"ok":true,"count":2}` {
		t.Fatalf("colored JSON text = %q", plain)
	}
}

func inspectorWithOutput(size int) Model {
	return inspectorWithOutputs(size, 1)
}

func inspectorWithOutputs(size, count int) Model {
	line := `{"event":"build","ok":true,"path":"src/main.go"}
`
	output := strings.Repeat(line, size/len(line)+1)[:size]
	rows, visible := make([]row, count), make([]int, count)
	for i := range count {
		rows[i] = row{
			label: "Bash", kind: kindTool,
			node: &session.Node{Output: output, Span: otlp.Span{SpanID: strconv.Itoa(i)}},
		}
		visible[i] = i
	}
	return Model{
		rows:        rows,
		visibleRows: visible,
		width:       120,
		height:      40,
		split:       50,
		place:       placeBottom,
		pane:        viewport.New(80, 20),
	}.sized().refresh()
}

func TestInspectorLoadsOutputAsReaderScrolls(t *testing.T) {
	m := inspectorWithOutput(128 * 1024)
	if m.paneShown != inspectorChunkBytes || m.paneTotal != 128*1024 {
		t.Fatalf("initial bytes = %d of %d", m.paneShown, m.paneTotal)
	}
	for m.paneShown < m.paneTotal {
		before := m.paneShown
		m.pane.GotoBottom()
		m = m.scrollPane(1)
		if m.paneShown <= before {
			t.Fatalf("loaded bytes stayed at %d", m.paneShown)
		}
	}
	if output := m.rows[0].node.Output; !strings.Contains(m.rows[0].raw(), output) {
		t.Fatal("raw row lost output bytes")
	}
}

func TestInspectorRefreshPreservesScrollAndDetectsReplacement(t *testing.T) {
	m := inspectorWithOutput(64 * 1024)
	m.pane.SetYOffset(10)
	before := m.pane.YOffset
	unchanged := m.refresh()
	if unchanged.pane.YOffset != before {
		t.Fatalf("unchanged refresh offset = %d, want %d", unchanged.pane.YOffset, before)
	}

	m.rows[0].node.Output = strings.Repeat("x", len(m.rows[0].node.Output))
	m.dataRev++
	replaced := m.refresh()
	if replaced.paneVersion == m.paneVersion {
		t.Fatal("same-size replacement kept stale pane version")
	}
	if replaced.pane.YOffset != before {
		t.Fatalf("replacement offset = %d, want %d", replaced.pane.YOffset, before)
	}
}

func TestInspectorRowChangeResetsLoadedBudget(t *testing.T) {
	m := inspectorWithOutputs(128*1024, 2)
	m.paneLoaded = m.paneTotal
	m = m.renderPane(m.paneIdentity(), m.currentPaneVersion(), false)
	m.cursor = 1
	m = m.refresh()
	if m.paneLoaded != inspectorChunkBytes || m.paneShown != inspectorChunkBytes {
		t.Fatalf("new row loaded %d bytes and showed %d", m.paneLoaded, m.paneShown)
	}
}

func TestInspectorSharesLoadBudgetAcrossMarkedRows(t *testing.T) {
	m := inspectorWithOutputs(31*1024, 2)
	m.rows[1].node.Output = strings.Repeat("B", 31*1024)
	m.marks = map[string]bool{"0": true, "1": true}
	m.marksChanged()
	m.paneKey, m.paneSelect = "", ""
	m = m.refresh()
	src, shown, total := m.paneSource()
	if shown != inspectorChunkBytes || total != 62*1024 {
		t.Fatalf("marked bytes = %d of %d", shown, total)
	}
	if got := strings.Count(ansi.Strip(src), "B"); got > 2048 {
		t.Fatalf("second row rendered %d bytes beyond the shared budget", got)
	}
}

func TestInspectorResizePreservesLoadedOutputAndScroll(t *testing.T) {
	m := inspectorWithOutput(128 * 1024)
	m.paneLoaded = m.paneTotal
	m = m.renderPane(m.paneIdentity(), m.currentPaneVersion(), false)
	m.pane.SetYOffset(20)
	loaded, offset := m.paneLoaded, m.pane.YOffset
	m.width += 20
	m = m.sized().clamp()
	if m.paneLoaded != loaded || m.pane.YOffset != offset {
		t.Fatalf("resize changed loaded bytes %d -> %d or offset %d -> %d",
			loaded, m.paneLoaded, offset, m.pane.YOffset)
	}
}

func TestInspectorMarkedFilterChangeResetsState(t *testing.T) {
	m := inspectorWithOutputs(128*1024, 2)
	m.marks = map[string]bool{"0": true, "1": true}
	m.marksChanged()
	m = m.refresh()
	m.paneLoaded = m.paneTotal
	m = m.renderPane(m.paneIdentity(), m.currentPaneVersion(), false)
	m.pane.SetYOffset(20)
	before := m.paneSelect
	m.rows, m.visibleRows = m.rows[1:], []int{0}
	m.reindexMarks()
	m.dataRev++
	m = m.refresh()
	if m.paneSelect == before {
		t.Fatal("different marked filter result kept the same pane selection")
	}
	if m.paneLoaded != inspectorChunkBytes || m.pane.YOffset != 0 {
		t.Fatalf("filtered selection kept %d loaded bytes and offset %d", m.paneLoaded, m.pane.YOffset)
	}
}

func TestInspectorTabNameSurvivesAvailabilityChange(t *testing.T) {
	m := inspectorWithOutput(64 * 1024)
	m.tab = "body"
	m = m.refresh()
	m.pane.SetYOffset(10)
	before := m.paneSelect
	m.rows[0].node.Patch = `--- old
+++ new
@@ -1 +1 @@
-old
+new
`
	m.dataRev++
	m = m.refresh()
	if m.tabName() != "body" || m.paneSelect != before || m.pane.YOffset != 10 {
		t.Fatalf("added tab selected %q with identity %q and offset %d", m.tabName(), m.paneSelect, m.pane.YOffset)
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

func TestInspectorSessionChangeResetsState(t *testing.T) {
	m := inspectorWithOutput(128 * 1024)
	m.current = &session.Session{Key: "service/one"}
	m = m.refresh()
	m.paneLoaded = m.paneTotal
	m = m.renderPane(m.paneIdentity(), m.currentPaneVersion(), false)
	m.pane.SetYOffset(20)
	m.current = &session.Session{Key: "service/two"}
	m.dataRev++
	m = m.refresh()
	if m.paneLoaded != inspectorChunkBytes || m.pane.YOffset != 0 {
		t.Fatalf("new session kept %d loaded bytes and offset %d", m.paneLoaded, m.pane.YOffset)
	}
}

func TestInspectorEndLoadsAllOutput(t *testing.T) {
	m := inspectorWithOutput(128 * 1024)
	m.focus = winPane
	m = m.toEnd()
	if m.paneShown != m.paneTotal || !m.pane.AtBottom() {
		t.Fatalf("inspector end showed %d of %d bytes at offset %d", m.paneShown, m.paneTotal, m.pane.YOffset)
	}
}

func TestWrapToPreservesUnicode(t *testing.T) {
	input := "a界b🙂c界d"
	wrapped := strings.Join(wrapTo(input, 3), "")
	if !utf8.ValidString(wrapped) || wrapped != input {
		t.Fatalf("wrapped text = %q", wrapped)
	}
}

func benchmarkInspectorRefresh(b *testing.B, size int) {
	m := inspectorWithOutput(size)
	if m.paneShown > inspectorChunkBytes {
		b.Fatalf("initial inspector rendered %d bytes", m.paneShown)
	}
	if m.pane.TotalLineCount() > 2000 {
		b.Fatalf("initial inspector rendered %d lines", m.pane.TotalLineCount())
	}
	b.SetBytes(int64(m.paneShown))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		one := m
		one.paneKey = ""
		_ = one.refresh()
	}
}

func BenchmarkInspectorRefresh32KiB(b *testing.B) {
	benchmarkInspectorRefresh(b, 32*1024)
}

func BenchmarkInspectorRefresh1MiB(b *testing.B) {
	benchmarkInspectorRefresh(b, 1024*1024)
}

func BenchmarkInspectorSelectionAfter1MiBLoaded(b *testing.B) {
	m := inspectorWithOutputs(1024*1024, 2)
	m.paneLoaded = m.paneTotal
	m = m.renderPane(m.paneIdentity(), m.currentPaneVersion(), false)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		one := m
		one.cursor = 1
		_ = one.refresh()
	}
}

func BenchmarkInspectorRender1MiB(b *testing.B) {
	m := inspectorWithOutput(1024 * 1024)
	b.SetBytes(int64(m.paneTotal))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		one := m
		one.paneLoaded = one.paneTotal
		_ = one.renderPane(one.paneIdentity(), one.currentPaneVersion(), false)
	}
}

func BenchmarkInspectorLoad1MiBProgressively(b *testing.B) {
	m := inspectorWithOutput(1024 * 1024)
	b.SetBytes(int64(m.paneTotal - m.paneShown))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		one := m
		for one.paneShown < one.paneTotal {
			one.pane.GotoBottom()
			one = one.scrollPane(1)
		}
	}
}

func benchmarkInspectorMarkedRows(b *testing.B, count int) {
	m := inspectorWithOutputs(32*1024, count)
	m.marks = make(map[string]bool, len(m.rows))
	for i := range m.rows {
		m.marks[m.idOf(i)] = true
	}
	m.marksChanged()
	m.paneKey, m.paneSelect = "", ""
	m = m.refresh()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		one := m
		one.paneKey = ""
		_ = one.refresh()
	}
}

func BenchmarkInspectorRefresh1000MarkedRows(b *testing.B) {
	benchmarkInspectorMarkedRows(b, 1000)
}

func BenchmarkInspectorRefresh35000MarkedRows(b *testing.B) {
	benchmarkInspectorMarkedRows(b, 35000)
}

func BenchmarkTraceView35000MarkedRows(b *testing.B) {
	m := inspectorWithOutputs(32*1024, 35000)
	m.marks = make(map[string]bool, len(m.rows))
	for i := range m.rows {
		m.marks[m.idOf(i)] = true
	}
	m.marksChanged()
	m = m.refresh()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.View()
	}
}

func BenchmarkInspectorMarkAll35000Rows(b *testing.B) {
	m := inspectorWithOutputs(32*1024, 35000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		one := m
		one.markAll()
		_ = one.refresh()
	}
}
