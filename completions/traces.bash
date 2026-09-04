__traces_completion_values_0() {
  printf '%s\n' 'auto' 'always' 'never'
}
__traces_completion_values_1() {
  'traces' 'provider' 'complete' "${COMP_LINE:0:COMP_POINT}" 2>/dev/null || true
}
__traces_completion_values_2() {
  'traces' 'provider' 'complete' "${COMP_LINE:0:COMP_POINT}" 2>/dev/null || true
}
__traces_completion_values_3() {
  'traces' 'provider' 'complete' "${COMP_LINE:0:COMP_POINT}" 2>/dev/null || true
}
__traces_completion_filter() {
  local prefix="$1"
  local prepend="${2-}"
  local candidate
  local existing
  local duplicate
  COMPREPLY=()
  while IFS= read -r candidate || [[ -n "$candidate" ]]; do
    [[ "$candidate" == "$prefix"* ]] || continue
    candidate="$prepend$candidate"
    duplicate=0
    for existing in "${COMPREPLY[@]}"; do
      if [[ "$existing" == "$candidate" ]]; then
        duplicate=1
        break
      fi
    done
    (( duplicate )) || COMPREPLY+=("$candidate")
  done
}

_traces_complete() {
  local current="${COMP_WORDS[COMP_CWORD]}"
  local previous=""
  local context=""
  local word
  local index
  local consume_value=0
  local options_done=0
  if (( COMP_CWORD > 0 )); then
    previous="${COMP_WORDS[COMP_CWORD-1]}"
  fi
  for ((index=1; index<COMP_CWORD; index++)); do
    word="${COMP_WORDS[index]}"
    if (( consume_value )); then
      consume_value=0
      continue
    fi
    if (( options_done )); then
      continue
    fi
    if [[ "$word" == '--' ]]; then
      options_done=1
      continue
    fi
    case "$context:$word" in
      ':--color') consume_value=1; continue ;;
      ':--color='*) continue ;;
      ':--config') consume_value=1; continue ;;
      ':--config='*) continue ;;
      ':--file') consume_value=1; continue ;;
      ':--file='*) continue ;;
      ':--lag') consume_value=1; continue ;;
      ':--lag='*) continue ;;
      ':--poll') consume_value=1; continue ;;
      ':--poll='*) continue ;;
      ':--provider') consume_value=1; continue ;;
      ':--provider='*) continue ;;
      ':--service') consume_value=1; continue ;;
      ':--service='*) continue ;;
      ':--session') consume_value=1; continue ;;
      ':--session='*) continue ;;
      ':--since') consume_value=1; continue ;;
      ':--since='*) continue ;;
      'provider list:--config') consume_value=1; continue ;;
      'provider list:--config='*) continue ;;
      'provider validate:--config') consume_value=1; continue ;;
      'provider validate:--config='*) continue ;;
    esac
    case "$context:$word" in
      ':completion') context='completion' ;;
      ':generate') context='generate' ;;
      ':provider') context='provider' ;;
      'provider:list') context='provider list' ;;
      'provider:validate') context='provider validate' ;;
    esac
  done
  case "$context:$previous" in
    ':--color') __traces_completion_filter "$current" < <(__traces_completion_values_0); return ;;
    ':--provider') __traces_completion_filter "$current" < <(__traces_completion_values_1); return ;;
  esac
  case "$context:$current" in
    ':--color='*) __traces_completion_filter "${current#*=}" "--color=" < <(__traces_completion_values_0); return ;;
    ':--provider='*) __traces_completion_filter "${current#*=}" "--provider=" < <(__traces_completion_values_1); return ;;
  esac
  case "$context" in
    '')
      __traces_completion_filter "$current" < <(
        printf '%s\n' 'completion' 'generate' 'provider' '--all' '--color' '--config' '--file' '--json' '--lag' '--list' '--once' '--poll' '--provider' '--service' '--session' '--since'
      )
      ;;
    'completion')
      __traces_completion_filter "$current" < <(
        printf '%s\n' 'bash' 'zsh' 'fish' 'nu'
      )
      ;;
    'generate')
      __traces_completion_filter "$current" < <(
        printf '%s\n' '--check'
      )
      ;;
    'provider')
      __traces_completion_filter "$current" < <(
        printf '%s\n' 'list' 'validate'
      )
      ;;
    'provider list')
      __traces_completion_filter "$current" < <(
        printf '%s\n' '--config' '--json' '--names'
        __traces_completion_values_2
      )
      ;;
    'provider validate')
      __traces_completion_filter "$current" < <(
        printf '%s\n' '--config' '--json'
        __traces_completion_values_3
      )
      ;;
  esac
}
complete -F _traces_complete traces