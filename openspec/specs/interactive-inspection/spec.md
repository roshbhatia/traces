> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define the live trace tree, inspector, navigation, and update behavior of Traces.

## Requirements

### Requirement: Live immutable updates

Interactive mode MUST feed source batches into the session store and present
immutable snapshots to the UI. It MUST ignore empty batches, serialize rebuilds,
and coalesce a newer batch while a rebuild is active.

#### Scenario: Poll returns no activity

- **WHEN** an interactive source emits an empty batch
- **THEN** the UI MUST retain its current snapshot without rebuilding rows

#### Scenario: Update arrives during a rebuild

- **WHEN** a newer batch arrives while the UI is building rows
- **THEN** the UI MUST process the newest pending state after the active rebuild

### Requirement: Activity actors and navigation

The tree MUST label user turns as `@user`, the main agent as `+main`, and
explicit or named agent paths as their corresponding lanes. The UI MUST support
session selection, filtering, folding, marks, ranges, follow mode, and focused
tree or inspector navigation.

#### Scenario: User changes focused pane

- **WHEN** the user presses the configured tree or inspector focus key
- **THEN** subsequent navigation MUST act on the selected pane

#### Scenario: Session remains available after an update

- **WHEN** the current or pinned session remains in a new snapshot
- **THEN** the UI MUST retain that selection instead of jumping to the newest session

### Requirement: Conditional inspector

The inspector MUST expose body, attributes, and changes tabs only when the
selected node has corresponding content. Large content MUST load in bounded
chunks.

#### Scenario: Edit node contains a patch

- **WHEN** the selected node has normalized patch content
- **THEN** the inspector MUST expose a changes tab

#### Scenario: Node has no attributes

- **WHEN** the selected node has no attribute content
- **THEN** the inspector MUST NOT expose an empty attributes tab

### Requirement: Live provider failure recovery

Interactive provider following MUST retry fetch failures on later polls without
terminating the interface.

#### Scenario: Provider poll fails

- **WHEN** an activity provider returns a transient fetch error
- **THEN** Traces MUST keep the interface running and try that provider on a later poll
