package provider

// These fixtures keep capability-specific inputs and the shared manifest
// vocabulary checked by `cue vet ./schema`.
activityFixture: #Manifest & {
	version: "provider/v1"
	name: "activity-fixture"
	description: "fixture"
	command: ["fixture"]
	actions: "activity.read": {
		description: "read activity"
		argv: ["--since", "{{ .Since }}"]
	}
}

diffFixture: #Manifest & {
	version: "provider/v1"
	name: "diff-fixture"
	description: "fixture"
	command: ["fixture"]
	actions: "diff.render": {
		description: "render diff"
		argv: ["{{ .Local }}", "{{ .Remote }}"]
	}
}

allActionsFixture: #Manifest & {
	version:     "provider/v1"
	name:        "all-actions-fixture"
	description: "fixture"
	command:     ["fixture"]
	actions: {
		"activity.read": {
			description: "read activity"
		}
		"clipboard.write": {
			description: "write to the clipboard"
		}
		"diff.render": {
			description: "render a diff"
		}
		"document.open": {
			description: "open a document"
		}
		"provider.validate": {
			description: "validate a provider"
		}
		"session.current": {
			description: "read the current session"
		}
		"session.discover": {
			description: "discover sessions"
		}
	}
}
