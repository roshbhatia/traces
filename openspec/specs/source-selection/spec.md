> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define how Traces configures, reads, merges, filters, and follows activity sources.

## Requirements

### Requirement: Configuration precedence

Traces MUST load YAML configuration from the configured path or its standard
user location. Matching `TRACES_*` environment values MUST override YAML, and
explicit command-line values MUST control the requested invocation.
Configuration MUST cover color, provider discovery, harness source mappings,
and diff, clipboard, and editor provider selection.

#### Scenario: Environment overrides YAML

- **WHEN** YAML and a matching environment variable set different values
- **THEN** Traces MUST use the environment value

### Requirement: Composable sources

Traces MUST accept a local telemetry file, standard input, configured providers,
or an explicit provider. It MUST merge all selected source batches before
presentation and MUST fetch independent providers concurrently.

#### Scenario: Local and provider data are selected

- **WHEN** an invocation selects both a local source and providers
- **THEN** Traces MUST present their spans and records as one combined input

#### Scenario: One provider serves several harness mappings

- **WHEN** configuration maps one provider to more than one harness
- **THEN** Traces MUST fetch that provider once per collection window

### Requirement: Service filtering

A service filter MUST apply to spans and records. A matching service name MUST
equal the filter or start with its prefix.

#### Scenario: Batch contains unrelated services

- **WHEN** a service filter is active
- **THEN** Traces MUST omit spans and records whose service does not match the filter prefix

### Requirement: Continuous following

Interactive file following MUST retain partial lines, detect truncation, and
reopen the source. Provider following MUST overlap collection windows, deduplicate
previously seen spans and records, and retry a failed fetch on a later poll.

#### Scenario: Telemetry file is truncated

- **WHEN** the followed file becomes smaller than the current read offset
- **THEN** Traces MUST reopen it and continue from the new file contents

#### Scenario: Provider repeats an event

- **WHEN** an overlapping provider window returns an already seen identity
- **THEN** Traces MUST emit that span or record only once to the interactive model
