> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define how Traces discovers sessions, attaches an invocation, and scopes session views.

## Requirements

### Requirement: Invocation attachment

An explicit session MUST take precedence over automatic attachment. `--all`
MUST disable session attachment. Otherwise, Traces MUST use capable providers
to discover sessions for the current directory and resolve the current session.

#### Scenario: Explicit session is supplied

- **WHEN** a user supplies a session identifier
- **THEN** Traces MUST pin the invocation to that identifier

#### Scenario: All sessions are requested

- **WHEN** a user supplies `--all`
- **THEN** Traces MUST NOT constrain the invocation to a current session

### Requirement: Ordered provider discovery

Current-session lookup MUST try providers by descending priority and use the
first non-empty identifier. Session discovery MUST use priority and provider
name order, remove duplicate identifiers, and preserve encounter order.

#### Scenario: Higher-priority current provider succeeds

- **WHEN** several current-session providers can return an identifier
- **THEN** Traces MUST use the first non-empty result in provider priority order

#### Scenario: One discovery provider fails

- **WHEN** a session discovery provider returns an error
- **THEN** Traces MUST continue discovery through the remaining providers

### Requirement: Directory and identity scope

A session MUST be in directory scope when its canonical working directory is
the selected directory or a descendant. Explicit session identifiers MUST NOT
be guessed or merged with another session.

#### Scenario: Session runs below the selected directory

- **WHEN** the session working directory is a descendant of the selected directory
- **THEN** Traces MUST include the session in that directory scope

#### Scenario: Explicit sessions have adjacent activity

- **WHEN** two explicit session identifiers have close timestamps
- **THEN** Traces MUST keep them as separate sessions

### Requirement: Stable session selection

Traces MUST support exact identifiers and keys, unique identifier prefixes, and
unique key suffixes. Interactive updates MUST retain a pinned or current
selection while that session remains available.

#### Scenario: Selector is ambiguous

- **WHEN** a prefix or suffix matches more than one session
- **THEN** Traces MUST NOT select either session as a unique match
