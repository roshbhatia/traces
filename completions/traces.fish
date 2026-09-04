complete -c traces -e
complete -c traces -f
function __traces_completion_values_0
  begin
    printf '%s\n' 'auto' 'always' 'never'
  end | string match -rv '\t'; or true
end
function __traces_completion_values_1
  begin
    command 'traces' 'provider' 'complete' (commandline -cp) 2>/dev/null; or true
  end | string match -rv '\t'; or true
end
function __traces_completion_values_2
  begin
    command 'traces' 'provider' 'complete' (commandline -cp) 2>/dev/null; or true
  end | string match -rv '\t'; or true
end
function __traces_completion_values_3
  begin
    command 'traces' 'provider' 'complete' (commandline -cp) 2>/dev/null; or true
  end | string match -rv '\t'; or true
end

function __traces_completion_context
  set -l context ''
  set -l words (commandline -opc)
  set -l consume_value 0
  set -l options_done 0
  for word in $words[2..-1]
    if test $consume_value -eq 1
      set consume_value 0
      continue
    end
    if test $options_done -eq 1
      continue
    end
    if test "$word" = '--'
      set options_done 1
      continue
    end
    switch "$context:$word"
      case ':--color'
        set consume_value 1
        continue
      case ':--color=*'
        continue
      case ':--config'
        set consume_value 1
        continue
      case ':--config=*'
        continue
      case ':--file'
        set consume_value 1
        continue
      case ':--file=*'
        continue
      case ':--lag'
        set consume_value 1
        continue
      case ':--lag=*'
        continue
      case ':--poll'
        set consume_value 1
        continue
      case ':--poll=*'
        continue
      case ':--provider'
        set consume_value 1
        continue
      case ':--provider=*'
        continue
      case ':--service'
        set consume_value 1
        continue
      case ':--service=*'
        continue
      case ':--session'
        set consume_value 1
        continue
      case ':--session=*'
        continue
      case ':--since'
        set consume_value 1
        continue
      case ':--since=*'
        continue
      case 'provider list:--config'
        set consume_value 1
        continue
      case 'provider list:--config=*'
        continue
      case 'provider validate:--config'
        set consume_value 1
        continue
      case 'provider validate:--config=*'
        continue
    end
    switch "$context:$word"
      case ':completion'
        set context 'completion'
      case ':generate'
        set context 'generate'
      case ':provider'
        set context 'provider'
      case 'provider:list'
        set context 'provider list'
      case 'provider:validate'
        set context 'provider validate'
    end
  end
  echo $context
end
complete -c traces -n 'test (__traces_completion_context) = ""' -l all -d 'Show every local run'
complete -c traces -n 'test (__traces_completion_context) = ""' -f -l color -r -a '(__traces_completion_values_0)' -d 'Color output'
complete -c traces -n 'test (__traces_completion_context) = ""' -l config -r -d 'YAML configuration file'
complete -c traces -n 'test (__traces_completion_context) = ""' -l file -r -d 'Read an OTLP JSON file'
complete -c traces -n 'test (__traces_completion_context) = ""' -l json -d 'Print newline-delimited JSON'
complete -c traces -n 'test (__traces_completion_context) = ""' -l lag -r -d 'Provider overlap window'
complete -c traces -n 'test (__traces_completion_context) = ""' -l list -d 'List sessions'
complete -c traces -n 'test (__traces_completion_context) = ""' -l once -d 'Print one trace tree'
complete -c traces -n 'test (__traces_completion_context) = ""' -l poll -r -d 'Provider poll interval'
complete -c traces -n 'test (__traces_completion_context) = ""' -f -l provider -r -a '(__traces_completion_values_1)' -d 'Read named activity providers'
complete -c traces -n 'test (__traces_completion_context) = ""' -l service -r -d 'Filter by service'
complete -c traces -n 'test (__traces_completion_context) = ""' -l session -r -d 'Attach by session ID or prefix'
complete -c traces -n 'test (__traces_completion_context) = ""' -l since -r -d 'Initial provider window'
complete -c traces -f -n 'test (__traces_completion_context) = ""' -a completion -d 'Generate shell completions'
complete -c traces -f -n 'test (__traces_completion_context) = ""' -a generate -d 'Generate README command docs and JSON Schema'
complete -c traces -f -n 'test (__traces_completion_context) = ""' -a provider -d 'Inspect and validate external providers'
complete -c traces -f -n 'test (__traces_completion_context) = "completion"' -a 'bash zsh fish nu'
complete -c traces -n 'test (__traces_completion_context) = "generate"' -l check -d 'Fail when generated files are stale'
complete -c traces -f -n 'test (__traces_completion_context) = "provider"' -a list -d 'List discovered providers'
complete -c traces -f -n 'test (__traces_completion_context) = "provider"' -a validate -d 'Validate provider commands and protocol output'
complete -c traces -n 'test (__traces_completion_context) = "provider list"' -l config -r -d 'YAML configuration file'
complete -c traces -n 'test (__traces_completion_context) = "provider list"' -l json -d 'Print JSON'
complete -c traces -n 'test (__traces_completion_context) = "provider list"' -l names -d 'Print provider names, one per line'
complete -c traces -f -n 'test (__traces_completion_context) = "provider list"' -a '(__traces_completion_values_2)'
complete -c traces -n 'test (__traces_completion_context) = "provider validate"' -l config -r -d 'YAML configuration file'
complete -c traces -n 'test (__traces_completion_context) = "provider validate"' -l json -d 'Print JSON'
complete -c traces -f -n 'test (__traces_completion_context) = "provider validate"' -a '(__traces_completion_values_3)'