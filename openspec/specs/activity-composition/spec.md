> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define how Traces decodes telemetry and composes spans and records into activity trees.

## Requirements

### Requirement: Runtime input decoding

Traces MUST accept newline-delimited flat records and OTLP export objects.
Runtime decoding MUST ignore blank lines, non-object JSON, malformed JSON, and
flat spans without span identifiers while retaining valid records from the same
input.

#### Scenario: Input mixes valid and invalid lines

- **WHEN** a stream contains valid telemetry and malformed lines
- **THEN** Traces MUST decode the valid telemetry and skip the malformed lines

### Requirement: Stable identity and session propagation

Traces MUST deduplicate spans by span identifier. Interactive and one-shot tree
composition MUST map known session identifiers by trace and apply them to
sessionless items in the same trace before building sessions.

#### Scenario: Sessionless span shares an identified trace

- **WHEN** an interactive or one-shot tree batch contains identified and sessionless items with the same trace identifier
- **THEN** Traces MUST compose both items into the identified session

#### Scenario: Span is received again

- **WHEN** a later batch contains an existing span identifier
- **THEN** Traces MUST update the existing span instead of adding a duplicate node

### Requirement: Record and runtime attachment

Traces MUST attach user prompts to eligible roots, assistant text by request
identifier, and tool results by tool-use identifier. Activity spans MUST replace
matching raw runtime rows, while runtime data MUST remain available as facets.

#### Scenario: Activity and runtime spans share a request

- **WHEN** an activity span and runtime span share a request identifier
- **THEN** Traces MUST show one activity row with the runtime timing, metrics, and failure details attached

#### Scenario: Tool result matches a tool call

- **WHEN** a tool result carries the tool call's tool-use identifier
- **THEN** Traces MUST attach the result to that tool node

### Requirement: Deterministic tree construction

Traces MUST normalize known activity names into turn, model, tool, edit, note,
compact, delegate, or error roles. Missing parents MUST create pending synthetic
turns. Siblings MUST sort by start time and then span identifier.

#### Scenario: Child arrives before its parent

- **WHEN** a span references a parent that is not present
- **THEN** Traces MUST retain the child beneath a pending synthetic turn

#### Scenario: Two spans share a start time

- **WHEN** sibling spans have equal start times
- **THEN** Traces MUST order them by span identifier
