> The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY in this document are
> to be interpreted as described in RFC 2119.

## Purpose

Define the finite list, tree, and machine-readable reporting modes of Traces.

## Requirements

### Requirement: Finite reporting modes

`--list`, `--once`, and `--json` MUST read the selected sources, produce their
requested output, and terminate without starting the interactive interface.

#### Scenario: Session list is requested

- **WHEN** a user supplies `--list`
- **THEN** Traces MUST print the available sessions and terminate

#### Scenario: One tree snapshot is requested

- **WHEN** a user supplies `--once`
- **THEN** Traces MUST print one uncolored activity tree and terminate

### Requirement: Machine-readable output

JSON mode MUST emit normalized newline-delimited records. A selected session
MUST retain spans in its composed tree, lifted facets, and sessionless records
from the same service.

#### Scenario: JSON is filtered to a session

- **WHEN** a user supplies `--json` with a selected session
- **THEN** Traces MUST omit activity from other identified sessions and unrelated services

### Requirement: Report failure status

A local source read failure MUST return exit status 1. Provider fetch failures
MUST be reported while allowing successful sources to produce partial output.
One-shot tree output MUST return exit status 2 when any selected span failed.

#### Scenario: Local file cannot be read

- **WHEN** a finite report cannot read its local input
- **THEN** Traces MUST report the read error and exit with status 1

#### Scenario: One-shot mode cannot select a session

- **WHEN** `--once` resolves no session from its composed input
- **THEN** Traces MUST report that no session was found and exit with status 1

#### Scenario: One provider fails

- **WHEN** one provider fails and another selected source succeeds
- **THEN** Traces MUST report the provider error and still render the successful source

#### Scenario: Selected tree contains a failed span

- **WHEN** `--once` renders a session containing a failed span
- **THEN** Traces MUST exit with status 2 after printing the tree
