> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define how Traces discovers, selects, executes, and validates external providers.

## Requirements

### Requirement: Deterministic discovery

Traces MUST search configured and standard provider directories in precedence
order. The first manifest for a logical provider name MUST reserve that name,
even when that manifest is invalid.

#### Scenario: Two directories contain the same provider

- **WHEN** discovery finds the same logical name in two directories
- **THEN** Traces MUST retain only the first result by directory precedence

#### Scenario: The first manifest is invalid

- **WHEN** an invalid manifest shadows a valid lower-precedence manifest
- **THEN** Traces MUST report the invalid provider and MUST NOT activate the lower manifest

### Requirement: Capability-based selection

Providers MAY declare only `activity.read`, `session.current`,
`session.discover`, `diff.render`, `clipboard.write`, `document.open`, and
`provider.validate`. Traces MUST select providers by declared capability. An
explicit provider request MUST fail when the provider is missing or unsuitable.
A missing configured activity provider MUST warn and allow other sources to
continue.

#### Scenario: Explicit provider is unavailable

- **WHEN** a user names a provider that cannot supply the requested capability
- **THEN** Traces MUST return an error instead of selecting another provider

#### Scenario: Configured activity provider is unavailable

- **WHEN** one configured activity provider cannot be resolved
- **THEN** Traces MUST warn and MUST continue with the remaining sources

### Requirement: Direct provider execution

Traces MUST render provider commands as argument and environment templates. It
MUST execute the resulting argument vector directly without shell evaluation.
Manifest-relative executable and argument paths MUST resolve beside the provider
manifest. Bare executable names MUST resolve through `PATH`.

#### Scenario: Template values contain shell syntax

- **WHEN** a rendered argument contains shell metacharacters
- **THEN** Traces MUST pass the value as one argument and MUST NOT evaluate it as shell input

### Requirement: Isolated validation

Provider validation MUST use isolated home, temporary, cache, configuration,
and data directories. It MUST verify requirements and declared actions.
Validation MUST NOT perform clipboard or document-opening side effects.

#### Scenario: Provider declares a side-effect capability

- **WHEN** validation reaches a clipboard or document action
- **THEN** Traces MUST validate the rendered command without executing the side effect

#### Scenario: Provider output is malformed

- **WHEN** a validation action emits output outside its declared contract
- **THEN** validation MUST fail with the provider and action identified
