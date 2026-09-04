// traces shows an agent run as a folding trace tree, live, from the OTLP JSON
// the local collector writes. It attaches to a session that is already running,
// the way you would attach to a log.
//
// Additional activity sources are external providers declared in YAML or named
// by --provider. Providers add to the collector file rather than replacing it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/roshbhatia/go-utils/completion"
	"github.com/roshbhatia/go-utils/paths"
	"github.com/roshbhatia/traces/internal/otlp"
	"github.com/roshbhatia/traces/internal/session"
	"github.com/roshbhatia/traces/internal/source"
	"github.com/roshbhatia/traces/internal/ui"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "completion" {
		generateCompletion(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "generate" {
		runGenerate(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "provider" {
		runProvider(os.Args[2:])
		return
	}
	configPath := argumentValue(os.Args[1:], "config")
	settings, err := source.LoadSettings(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	registry, err := source.Discover(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	file := flag.String("file", "", "OTLP JSON file to read (default: the collector's)")
	flag.String("config", configPath, "configuration file (default: ~/.config/traces/config.yaml)")
	pinned := flag.String("session", "", "attach to this session, by id or prefix")
	list := flag.Bool("list", false, "list the sessions and exit")
	once := flag.Bool("once", false, "print the tree once and exit; status 2 when a span failed")
	asked := flag.String("provider", "", "read exactly these sources, comma separated, instead of the ones declared in "+source.ConfigFile(configPath))
	back := flag.Duration("since", 2*time.Hour, "with a provider, how far back the first read reaches")
	every := flag.Duration("poll", 15*time.Second, "with a provider, how often to re-read")
	lag := flag.Duration("lag", 90*time.Second, "with a provider, how much every poll overlaps the last")
	all := flag.Bool("all", false, "show every run on this machine, not only this directory's")
	asJSON := flag.Bool("json", false, "print the spans as newline delimited JSON and exit")
	service := flag.String("service", "", "keep only this service, by name or prefix")
	color := flag.String("color", settings.Color, "color output: auto, always, or never")
	flag.Parse()
	switch *color {
	case "auto":
	case "always":
		lipgloss.SetColorProfile(termenv.ANSI)
	case "never":
		lipgloss.SetColorProfile(termenv.Ascii)
	default:
		fmt.Fprintln(os.Stderr, "traces: -color must be auto, always, or never")
		os.Exit(1)
	}

	// traces opens on the work in front of the reader. Inside an agent session
	// that is the session itself, and outside one it is whatever a provider found
	// for this directory.
	scope := []string{}
	directory := ""
	if !*all && *pinned == "" {
		if here, err := os.Getwd(); err == nil {
			directory = here
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			scope = registry.DiscoverSessions(ctx, here)
			cancel()
		}
	}
	which := attached(*pinned, *all, registry, directory)

	providers, err := source.ResolveRegistry(*asked, *service, settings, registry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	diffProvider, err := source.ResolveNamed(settings.Diff.Provider, source.ActionDiffRender, registry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: skipped diff provider: %v\n", err)
	}
	if diffProvider != nil {
		diffProvider.Color = providerColor(*color)
	}
	clipboardProvider, err := source.ResolveNamed(
		settings.Clipboard.Provider, source.ActionClipboardWrite, registry,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: skipped clipboard provider: %v\n", err)
	}
	documentProvider, err := source.ResolveNamed(
		settings.Editor.Provider, source.ActionDocumentOpen, registry,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: skipped document provider: %v\n", err)
	}
	for _, one := range providers {
		one.Session = which
		one.Directory = directory
	}

	path := *file
	if path == "" {
		path = paths.OtelTelemetry()
	}
	// A dash is the shell's own name for standard input, so a provider or a
	// saved capture pipes straight in: `activity-reader | traces --once`.
	if path == "-" {
		path = ""
	}

	// Providers add to the collector file rather than replacing it. This keeps
	// local telemetry and command-backed activity visible in one view.
	src := sources{
		path: path, providers: providers, back: *back, every: *every, lag: *lag,
		service: *service, diffProvider: diffProvider,
		clipboardProvider: clipboardProvider, documentProvider: documentProvider,
	}

	if *asJSON || *list || *once {
		os.Exit(src.report(which, scope, directory, *list, *asJSON))
	}
	os.Exit(src.watch(which, scope, directory))
}

var stdoutIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func providerColor(mode string) string {
	if mode != "auto" {
		return mode
	}
	if stdoutIsTerminal() {
		return "always"
	}
	return "never"
}

func generateCompletion(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "traces: completion requires bash, zsh, fish, or nu")
		os.Exit(1)
	}
	out, err := completion.Generate(args[0], commandMetadata())
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

func commandMetadata() completion.Command {
	return completion.Command{
		Name:        "traces",
		Description: "Inspect agent activity as a trace tree",
		Flags: []completion.Flag{
			{Name: "all", Description: "Show every local run"},
			{Name: "color", Description: "Color output", Value: true, Values: []string{"auto", "always", "never"}},
			{Name: "config", Description: "YAML configuration file", Value: true},
			{Name: "file", Description: "Read an OTLP JSON file", Value: true},
			{Name: "json", Description: "Print newline-delimited JSON"},
			{Name: "lag", Description: "Provider overlap window", Value: true},
			{Name: "list", Description: "List sessions"},
			{Name: "once", Description: "Print one trace tree"},
			{Name: "poll", Description: "Provider poll interval", Value: true},
			{
				Name: "provider", Description: "Read named activity providers", Value: true,
				CompletionCommand: providerCompletionCommand(),
			},
			{Name: "service", Description: "Filter by service", Value: true},
			{Name: "session", Description: "Attach by session ID or prefix", Value: true},
			{Name: "since", Description: "Initial provider window", Value: true},
		},
		Subcommands: []completion.Command{{
			Name:        "generate",
			Description: "Generate README command docs and JSON Schema",
			Flags: []completion.Flag{
				{Name: "check", Description: "Fail when generated files are stale"},
			},
		}, {
			Name:        "provider",
			Description: "Inspect and validate external providers",
			Subcommands: []completion.Command{
				{
					Name:              "list",
					Description:       "List discovered providers",
					Flags:             providerCommandFlags(true),
					CompletionCommand: providerCompletionCommand(),
				},
				{
					Name:              "validate",
					Description:       "Validate provider commands and protocol output",
					Flags:             providerCommandFlags(false),
					CompletionCommand: providerCompletionCommand(),
				},
			},
		}},
	}
}

func providerCommandFlags(includeNames bool) []completion.Flag {
	flags := []completion.Flag{
		{Name: "config", Description: "YAML configuration file", Value: true},
		{Name: "json", Description: "Print JSON"},
	}
	if includeNames {
		flags = append(flags, completion.Flag{Name: "names", Description: "Print provider names, one per line"})
	}
	return flags
}

func providerCompletionCommand() []string {
	return []string{"traces", "provider", "complete", completion.ContextPlaceholder}
}

func runProvider(args []string) {
	if len(args) > 0 && args[0] == "complete" {
		if len(args) != 2 {
			return
		}
		runProviderCompletion(args[1])
		return
	}
	if len(args) == 0 || (args[0] != "list" && args[0] != "validate") {
		fmt.Fprintln(os.Stderr, "traces: provider requires list or validate")
		os.Exit(1)
	}
	action := args[0]
	flags := flag.NewFlagSet("traces provider "+action, flag.ContinueOnError)
	configPath := flags.String("config", "", "YAML configuration file")
	asJSON := flags.Bool("json", false, "print JSON")
	namesOnly := false
	if action == "list" {
		flags.BoolVar(&namesOnly, "names", false, "print provider names, one per line")
	}
	flagArgs, selected, err := providerArguments(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	if err := flags.Parse(flagArgs); err != nil {
		os.Exit(1)
	}
	if namesOnly && *asJSON {
		fmt.Fprintln(os.Stderr, "traces: --names requires provider list without --json")
		os.Exit(1)
	}
	settings, err := source.LoadSettings(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	registry, discoveryIssues, err := source.DiscoverChecked(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	for _, issue := range discoveryIssues {
		fmt.Fprintf(os.Stderr, "traces: invalid provider manifest: %v\n", issue)
	}
	names := registry.Names()
	if selected != "" {
		if _, ok := registry[selected]; !ok {
			fmt.Fprintf(os.Stderr, "traces: unknown provider %q\n", selected)
			os.Exit(1)
		}
		names = []string{selected}
	}
	if action == "list" {
		if namesOnly {
			for _, name := range names {
				fmt.Println(name)
			}
			exitForDiscoveryIssues(discoveryIssues)
			return
		}
		if *asJSON {
			data, _ := json.Marshal(registry)
			fmt.Println(string(data))
			exitForDiscoveryIssues(discoveryIssues)
			return
		}
		if len(names) == 0 {
			fmt.Println("No providers were discovered.")
			exitForDiscoveryIssues(discoveryIssues)
			return
		}
		for _, name := range names {
			loaded := registry[name]
			manifest := loaded.Manifest
			description := manifest.Description
			if description == "" {
				description = "Agent activity source"
			}
			actions := make([]string, 0, len(manifest.Actions))
			for action := range manifest.Actions {
				actions = append(actions, action)
			}
			slices.Sort(actions)
			output, err := renderProviderList(providerListView{
				Name: name, Description: description, Source: loaded.Path,
				Provides: strings.Join(actions, ", "), Command: strings.Join(manifest.Command, " "),
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "traces: render provider list: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(output)
		}
		exitForDiscoveryIssues(discoveryIssues)
		return
	}
	directory, _ := os.Getwd()
	if len(names) == 0 {
		if *asJSON {
			fmt.Println("[]")
		} else {
			fmt.Println("No providers were discovered.")
		}
		exitForDiscoveryIssues(discoveryIssues)
		return
	}
	results := make([]source.Validation, 0, len(names))
	failed := len(discoveryIssues) > 0
	for _, name := range names {
		result := source.Validate(context.Background(), name, registry[name], directory)
		results = append(results, result)
		failed = failed || result.Status != "ok"
	}
	if *asJSON {
		data, _ := json.Marshal(results)
		fmt.Println(string(data))
	} else {
		for _, result := range results {
			mark := "+"
			if result.Status != "ok" {
				mark = "x"
			}
			fmt.Printf("%s %s · %s\n", mark, result.Name, strings.Join(result.Provides, ", "))
			for _, check := range result.Checks {
				checkMark := "+"
				if check.Status != "ok" {
					checkMark = "x"
				}
				fmt.Printf("  %s %-12s %s\n", checkMark, check.Name, check.Message)
			}
		}
	}
	if failed {
		os.Exit(1)
	}
}

func runProviderCompletion(context string) {
	settings, err := source.LoadSettings("")
	if err != nil {
		return
	}
	registry, _, err := source.DiscoverChecked(settings)
	if err != nil {
		return
	}
	for _, candidate := range providerCompletionCandidates(context, registry.Names()) {
		fmt.Println(candidate)
	}
}

func providerCompletionCandidates(context string, names []string) []string {
	value := completionValue(context)
	comma := strings.LastIndex(value, ",")
	prefix := ""
	current := value
	selected := map[string]bool{}
	if comma >= 0 {
		prefix = value[:comma+1]
		current = value[comma+1:]
		for _, name := range strings.Split(strings.TrimSuffix(prefix, ","), ",") {
			selected[name] = true
		}
	}
	candidates := make([]string, 0, len(names))
	for _, name := range names {
		if !selected[name] && strings.HasPrefix(name, current) {
			candidates = append(candidates, prefix+name)
		}
	}
	return candidates
}

func completionValue(context string) string {
	if context == "" || strings.ContainsAny(context[len(context)-1:], " \t\r\n") {
		return ""
	}
	start := strings.LastIndexAny(context, " \t\r\n") + 1
	value := context[start:]
	if _, after, found := strings.Cut(value, "="); found {
		return after
	}
	return value
}

func exitForDiscoveryIssues(issues []error) {
	if len(issues) > 0 {
		os.Exit(1)
	}
}

func providerArguments(args []string) ([]string, string, error) {
	flags := make([]string, 0, len(args))
	name := ""
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if strings.HasPrefix(argument, "-") {
			flags = append(flags, argument)
			if argument == "-config" || argument == "--config" {
				index++
				if index >= len(args) {
					return nil, "", fmt.Errorf("%s requires a value", argument)
				}
				flags = append(flags, args[index])
			}
			continue
		}
		if name != "" {
			return nil, "", fmt.Errorf("provider command accepts at most one provider name")
		}
		name = argument
	}
	return flags, name, nil
}

type providerListView struct {
	Name        string
	Description string
	Source      string
	Provides    string
	Command     string
}

var providerListTemplate = template.Must(template.New("provider list").Option("missingkey=error").Parse(`{{ .Name }}
  {{ .Description }}
  source    {{ .Source }}
  provides  {{ .Provides }}
  command   {{ .Command }}
`))

func renderProviderList(view providerListView) (string, error) {
	var output strings.Builder
	if err := providerListTemplate.Execute(&output, view); err != nil {
		return "", err
	}
	return output.String(), nil
}

func runGenerate(args []string) {
	flags := flag.NewFlagSet("traces generate", flag.ContinueOnError)
	check := flags.Bool("check", false, "fail when generated files are stale")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "traces: generate accepts only flags")
		os.Exit(1)
	}
	schema, err := source.Schema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	providerSchema, err := source.ProviderSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: read README.md: %v\n", err)
		os.Exit(1)
	}
	generated, err := completion.ReplaceSection(string(readme), "cli", completion.Markdown(commandMetadata()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		os.Exit(1)
	}
	outputs := map[string][]byte{
		"README.md":                   []byte(generated),
		"schema/traces.schema.json":   schema,
		"schema/provider.schema.json": providerSchema,
	}
	completionPaths := map[string]string{
		"bash": "completions/traces.bash",
		"fish": "completions/traces.fish",
		"nu":   "completions/traces.nu",
		"zsh":  "completions/_traces",
	}
	for shell, path := range completionPaths {
		generated, err := completion.Generate(shell, commandMetadata())
		if err != nil {
			fmt.Fprintf(os.Stderr, "traces: generate %s completion: %v\n", shell, err)
			os.Exit(1)
		}
		outputs[path] = []byte(generated)
	}
	for path, data := range outputs {
		if *check {
			current, readErr := os.ReadFile(path)
			if readErr != nil || string(current) != string(data) {
				fmt.Fprintf(os.Stderr, "traces: %s is stale; run traces generate\n", path)
				os.Exit(1)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "traces: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "traces: %v\n", err)
			os.Exit(1)
		}
	}
}

func argumentValue(args []string, name string) string {
	long := "--" + name
	short := "-" + name
	for index, argument := range args {
		if value, ok := strings.CutPrefix(argument, long+"="); ok {
			return value
		}
		if value, ok := strings.CutPrefix(argument, short+"="); ok {
			return value
		}
		if (argument == long || argument == short) && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

// attached names the run to open. A flag wins, then the session this process
// was started inside.
func attached(pinned string, all bool, registry source.Registry, directory string) string {
	if pinned != "" {
		return pinned
	}
	if all {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return registry.CurrentSession(ctx, directory)
}

// sources is every place this machine keeps spans.
type sources struct {
	path              string
	providers         []*source.Provider
	back              time.Duration
	every             time.Duration
	lag               time.Duration
	service           string
	diffProvider      *source.Provider
	clipboardProvider *source.Provider
	documentProvider  *source.Provider
}

// name says what the frame is reading, so an empty view names the source that
// was empty rather than leaving the reader to guess which one.
func (s sources) name() string {
	where := s.path
	if where == "" {
		where = "standard input"
	}
	for _, one := range s.providers {
		where += " and " + one.Name
		// A source declared for one harness is read for that harness only, and
		// a header naming only the source hides which run it answered for.
		if who := one.For(); who != "" {
			where += "(" + who + ")"
		}
	}
	return where
}

// fetch runs every provider over the same window and folds the answers into one
// batch. A provider that fails is named and skipped: the others already
// answered, and one unreachable source should not empty the view.
func (s sources) fetch(ctx context.Context) otlp.Batch {
	type answer struct {
		batch otlp.Batch
		err   error
	}
	answers := make(chan answer, len(s.providers))
	for _, one := range s.providers {
		go func() {
			read, err := one.Fetch(ctx, s.back)
			answers <- answer{batch: read, err: err}
		}()
	}

	out := otlp.Batch{}
	for range s.providers {
		one := <-answers
		if one.err != nil {
			fmt.Fprintf(os.Stderr, "traces: %v\n", one.err)
			continue
		}
		out.Spans = append(out.Spans, one.batch.Spans...)
		out.Records = append(out.Records, one.batch.Records...)
	}
	return out
}

// read pulls every source into one batch. An empty path means standard input.
func (s sources) read() (otlp.Batch, error) {
	if s.path == "" {
		blob, err := io.ReadAll(os.Stdin)
		if err != nil {
			return otlp.Batch{}, err
		}
		return source.DecodeAny(blob), nil
	}
	blob, err := os.ReadFile(s.path)
	if err != nil {
		return otlp.Batch{}, err
	}
	return source.DecodeAny(blob), nil
}

// keep drops the services the reader did not ask for. A prefix matches related
// service variants without requiring their full telemetry name.
func (s sources) keep(in otlp.Batch) otlp.Batch {
	if s.service == "" {
		return in
	}
	out := otlp.Batch{}
	for _, one := range in.Spans {
		if strings.HasPrefix(one.Service, s.service) {
			out.Spans = append(out.Spans, one)
		}
	}
	for _, one := range in.Records {
		if strings.HasPrefix(one.Service, s.service) {
			out.Records = append(out.Records, one)
		}
	}
	return out
}

func (s sources) report(which string, scope []string, directory string, listing, asJSON bool) int {
	batch, err := s.read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		return 1
	}
	if len(s.providers) > 0 {
		// The file already answered, so a provider that cannot run costs its
		// own harness and not the whole view.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		read := s.fetch(ctx)
		cancel()
		batch.Spans = append(batch.Spans, read.Spans...)
		batch.Records = append(batch.Records, read.Records...)
	}
	batch = s.keep(batch)
	if asJSON {
		// A filter the reader gave has to reach every output, including JSON.
		if err := source.Encode(os.Stdout, only(batch, which, scope, directory)); err != nil {
			fmt.Fprintf(os.Stderr, "traces: %v\n", err)
			return 1
		}
		return 0
	}
	return show(batch, s.name(), which, scope, directory, listing)
}

// show is the non-interactive half of both sources, so a provider prints the
// same list and the same tree the file does.
func show(batch otlp.Batch, from, which string, scope []string, directory string, listing bool) int {
	store := session.NewStore()
	store.Scope(scope, directory)
	store.AddBatch(batch)

	if listing {
		fmt.Fprintf(os.Stderr, "traces: %d spans and %d records from %s\n", len(batch.Spans), len(batch.Records), from)
		list(os.Stdout, store.Sessions())
		return 0
	}

	found := pick(store, which)
	if found == nil {
		fmt.Fprintln(os.Stderr, "traces: no session in "+from)
		return 1
	}
	ui.Print(os.Stdout, found)
	// 2 for a run that holds a failed span, so a script can gate on it without
	// reading the tree. 1 stays "traces could not answer", which is the ordinary
	// meaning of 1 and the one a caller already handles.
	if failed(found) {
		return 2
	}
	return 0
}

func failed(one *session.Session) bool {
	var walk func(*session.Node) bool
	walk = func(n *session.Node) bool {
		if n.Span.Failed {
			return true
		}
		for _, kid := range n.Children {
			if walk(kid) {
				return true
			}
		}
		return false
	}
	for _, root := range one.Roots {
		if walk(root) {
			return true
		}
	}
	return false
}

// list sizes every column to the rows it holds. Fixed widths let long service
// names and trace keys shift the columns after them.
//
// The id column prints the short id, because that is what --session takes, and
// the name beside it is what the harness itself called the run. A list of six
// sessions used to read as six hex strings.
func list(w io.Writer, all []*session.Session) {
	service, id, count := 0, 0, 0
	for _, one := range all {
		service = max(service, len(one.Service))
		id = max(id, len(one.Short()))
		count = max(count, len(strconv.Itoa(one.ViewCount())))
	}
	for _, one := range all {
		shown := one.ViewCount()
		_, _ = fmt.Fprintf(w, "%-*s  %-*s  %*d %-5s  %s  %s\n",
			service, one.Service,
			id, one.Short(),
			count, shown, plural(shown, "item"),
			one.Last.Format("15:04:05"),
			// A run with no title and no prompt has no name, and printing the
			// id again put the same 8 characters in two columns.
			clip(named(one), 56))
	}
}

func named(one *session.Session) string {
	if name := one.Name(); name != one.Short() {
		return name
	}
	return ""
}

// A title runs as long as the harness wants, and a wrapped listing row loses
// the column alignment that makes the listing readable at all.
func clip(s string, width int) string {
	if len(s) <= width {
		return s
	}
	return s[:width-1] + "\u2026"
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// only narrows a batch to the run the reader named, by building the same store
// the tree is drawn from and reading back the spans that survived. Filtering the
// spans directly would need session grouping in two places, and the two would
// disagree the first time either changed.
func only(batch otlp.Batch, which string, scope []string, directory string) otlp.Batch {
	if which == "" && len(scope) == 0 {
		return batch
	}
	store := session.NewStore()
	store.Scope(scope, directory)
	store.Add(batch.Spans)
	store.AddRecords(batch.Records)
	found := pick(store, which)
	if found == nil {
		return otlp.Batch{}
	}
	keep := map[string]bool{}
	var walk func(*session.Node)
	walk = func(node *session.Node) {
		keep[node.Span.SpanID] = true
		for _, facet := range node.Facets {
			keep[facet.SpanID] = true
		}
		for _, kid := range node.Children {
			walk(kid)
		}
	}
	for _, root := range found.Roots {
		walk(root)
	}
	out := otlp.Batch{}
	for _, span := range batch.Spans {
		if keep[span.SpanID] {
			out.Spans = append(out.Spans, span)
		}
	}
	for _, one := range batch.Records {
		// A record with no session of its own belongs to this run only when its
		// service does.
		if one.Session == found.ID || (one.Session == "" && one.Service == found.Service) {
			out.Records = append(out.Records, one)
		}
	}
	return out
}

func pick(store *session.Store, which string) *session.Session {
	if which != "" {
		return store.Session(which)
	}
	all := store.Sessions()
	if len(all) == 0 {
		return nil
	}
	return all[0]
}

// watch follows every source at once. Each writes into the same channel, and
// the file's own closer is the one that ends it: a provider poll is slow and
// the file is the source that is always present.
func (s sources) watch(which string, scope []string, directory string) int {
	stop := make(chan struct{})
	batches := make(chan otlp.Batch, 32)

	fromFile := make(chan otlp.Batch, 32)
	if s.path != "" {
		go otlp.Follow(s.path, 400*time.Millisecond, fromFile, stop)
	} else {
		// Standard input is read once and ends. Following it would block the
		// view on a pipe that is already closed.
		go func() {
			defer close(fromFile)
			if batch, err := s.read(); err == nil && !batch.Empty() {
				fromFile <- batch
			}
		}()
	}

	// One channel per provider would need one select arm per provider, so each
	// follower writes into a shared channel and a counter closes it once.
	var fromProvider chan otlp.Batch
	if len(s.providers) > 0 {
		fromProvider = make(chan otlp.Batch, 32)
		each := make([]chan otlp.Batch, len(s.providers))
		for i, one := range s.providers {
			each[i] = make(chan otlp.Batch, 32)
			go source.Follow(*one, s.every, s.back, s.lag, each[i], stop)
		}
		var wg sync.WaitGroup
		for _, in := range each {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for batch := range in {
					fromProvider <- batch
				}
			}()
		}
		go func() {
			wg.Wait()
			close(fromProvider)
		}()
	}

	go func() {
		defer close(batches)
		for fromFile != nil || fromProvider != nil {
			select {
			case one, ok := <-fromFile:
				if !ok {
					fromFile = nil
					continue
				}
				batches <- s.keep(one)
			case one, ok := <-fromProvider:
				if !ok {
					fromProvider = nil
					continue
				}
				batches <- s.keep(one)
			}
		}
	}()

	return run(batches, stop, which, scope, directory, s.name(), ui.Providers{
		Clipboard: s.clipboardProvider,
		Diff:      s.diffProvider,
		Document:  s.documentProvider,
	})
}

// run owns the program either way. Follow owns its own goroutine, so the spans
// arrive as messages rather than as a blocking read inside Update.
func run(
	batches chan otlp.Batch,
	stop chan struct{},
	which string,
	scope []string,
	directory, from string,
	providers ui.Providers,
) int {
	store := session.NewStore()
	store.Scope(scope, directory)
	program := tea.NewProgram(
		ui.New(store, which, from, providers),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	go func() {
		for batch := range batches {
			program.Send(ui.BatchMsg(batch))
		}
	}()

	_, err := program.Run()
	close(stop)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traces: %v\n", err)
		return 1
	}
	return 0
}
