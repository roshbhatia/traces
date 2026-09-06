> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define external and built-in diff rendering with bounded, implementation-aware caching.

## Requirements

### Requirement: External rendering with fallback

When configured, Traces MUST offer a diff provider temporary local, remote, and
merged files together with width and color settings. Provider exit status 1
MUST represent a rendered difference. An error or empty result MUST fall back to
the built-in renderer.

#### Scenario: External renderer succeeds

- **WHEN** the diff provider returns non-empty output with status 0 or 1
- **THEN** Traces MUST display that output

#### Scenario: External renderer fails

- **WHEN** the diff provider times out, errors, or returns empty output
- **THEN** Traces MUST render the patch with the built-in renderer

### Requirement: Content-addressed cache identity

The diff cache key MUST include the patch, render width, and provider identity.
Provider identity MUST include its manifest and regular executable,
interpreter, and diff-action argument files.

#### Scenario: Provider implementation changes

- **WHEN** a hashed provider implementation file changes without a manifest change
- **THEN** Traces MUST use a different diff cache key

#### Scenario: View width changes

- **WHEN** the inspector renders the same patch at a different width
- **THEN** Traces MUST use a different diff cache key

### Requirement: Bounded memory and disk caches

Memory and disk diff caches MUST expire entries after seven days and retain no
more than 128 entries each. Disk cache writes MUST use an atomic rename from a
temporary file.

#### Scenario: Cache exceeds its entry limit

- **WHEN** a cache contains more than 128 eligible entries
- **THEN** Traces MUST evict the oldest entries until the limit is met

#### Scenario: Cached result is stale

- **WHEN** a cached diff is older than seven days
- **THEN** Traces MUST ignore and remove the stale entry
