> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define optional clipboard and document-opening actions without coupling core inspection to a desktop host.

## Requirements

### Requirement: Optional host capabilities

Clipboard and document actions MUST be available only through a selected
provider that declares the matching capability. Missing host-action providers
MUST NOT prevent trace inspection.

#### Scenario: Clipboard provider is absent

- **WHEN** a user requests a clipboard action without a capable provider
- **THEN** Traces MUST keep the session open and show that the action is unavailable

#### Scenario: Document provider is present

- **WHEN** a selected provider declares `document.open`
- **THEN** Traces MUST enable the document action for eligible content

### Requirement: Protected temporary content

Traces MUST write selected content to a temporary file with mode `0600` before
passing its path to a host-action provider.

#### Scenario: Host action receives content

- **WHEN** Traces prepares selected content for a clipboard or document action
- **THEN** the provider MUST receive a path to a file readable only by its owner

### Requirement: Host execution behavior

Clipboard actions MUST run asynchronously with a bounded context. Document
actions MUST use terminal handoff so the provider can own interactive terminal
control and return it afterward.

#### Scenario: Clipboard action is invoked

- **WHEN** a capable clipboard provider receives selected content
- **THEN** the UI MUST remain responsive while the bounded action runs

#### Scenario: Document action is invoked

- **WHEN** a capable document provider receives selected content
- **THEN** Traces MUST hand terminal control to the provider and restore the UI after it exits

### Requirement: Side-effect-free validation

Provider validation MUST render clipboard and document commands and MUST NOT
execute them.

#### Scenario: Host provider is validated

- **WHEN** validation checks declared clipboard or document capabilities
- **THEN** no external clipboard or document application MUST be changed or opened
