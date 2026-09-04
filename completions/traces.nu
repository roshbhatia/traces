export extern "traces" [
  --all # Show every local run
  --color: string@"__traces_completion_values_0" # Color output
  --config: string # YAML configuration file
  --file: string # Read an OTLP JSON file
  --json # Print newline-delimited JSON
  --lag: string # Provider overlap window
  --list # List sessions
  --once # Print one trace tree
  --poll: string # Provider poll interval
  --provider: string@"__traces_completion_values_1" # Read named activity providers
  --service: string # Filter by service
  --session: string # Attach by session ID or prefix
  --since: string # Initial provider window
  ...args: string@"__traces_completion_none"
]

export extern "traces completion" [
  shell: string@"nu-complete traces shell"
]

def "nu-complete traces shell" [] { [bash zsh fish nu] }

export extern "traces generate" [
  --check # Fail when generated files are stale
  ...args: string@"__traces_completion_none"
]

export extern "traces provider" [
  ...args: string@"__traces_completion_none"
]

export extern "traces provider list" [
  --config: string # YAML configuration file
  --json # Print JSON
  --names # Print provider names, one per line
  ...args: string@"__traces_completion_values_2"
]

export extern "traces provider validate" [
  --config: string # YAML configuration file
  --json # Print JSON
  ...args: string@"__traces_completion_values_3"
]

def "__traces_completion_none" [] { [] }

def "__traces_completion_values_0" [context?: string] {
  [
    "auto"
    "always"
    "never"
  ] | flatten | uniq
}

def "__traces_completion_values_1" [context?: string] {
  [
    (try { run-external "traces" "provider" "complete" ($context | default "") | lines } catch { [] })
  ] | flatten | uniq
}

def "__traces_completion_values_2" [context?: string] {
  [
    (try { run-external "traces" "provider" "complete" ($context | default "") | lines } catch { [] })
  ] | flatten | uniq
}

def "__traces_completion_values_3" [context?: string] {
  [
    (try { run-external "traces" "provider" "complete" ($context | default "") | lines } catch { [] })
  ] | flatten | uniq
}