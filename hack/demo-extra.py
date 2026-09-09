import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
ROOT = pathlib.Path(__file__).resolve().parents[1]
NAME = sys.argv[1]
META = json.loads((ROOT / 'extras' / NAME / 'demo.json').read_text())
TOOL = META['core']
OLD = 'package auth\nimport "strings"\nfunc ValidToken(header string) bool { return strings.Contains(header, "Bearer ") }\n'
NEW = 'package auth\nimport "strings"\nfunc ValidToken(header string) bool {\n if !strings.HasPrefix(header, "Bearer ") { return false }\n token := strings.TrimPrefix(header, "Bearer ")\n return token != "" && !strings.ContainsAny(token, " \\t\\n")\n}\n'
TEST = 'package auth\nimport "testing"\nfunc TestTokenBoundary(t *testing.T) {\n for _, tc := range []struct{header string; valid bool}{\n {"Bearer signed-token",true},{"",false},{"Bearer ",false},{"prefix Bearer token",false},{"Bearer two tokens",false},\n } { if got:=ValidToken(tc.header); got!=tc.valid {t.Errorf("%q: got %v want %v",tc.header,got,tc.valid)} }\n}\n'

def execute(argv, cwd, env, stdin=None, show=True, check=True):
    if show:
        display = [pathlib.Path(str(argv[0])).name, *[str(value).replace(str(cwd), '.') for value in argv[1:]]]
        print('$ ' + ' '.join(display), flush=True)
    result = subprocess.run([str(value) for value in argv], cwd=cwd, env=env, input=stdin, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    if show or result.returncode:
        print(result.stdout.rstrip().replace(str(cwd), '.'), flush=True)
    if check and result.returncode:
        raise RuntimeError('command failed: ' + str(result.returncode))
    return result

def build(package, binary, bins):
    module = ROOT
    source = ROOT / package.removeprefix('./')
    if (source / 'go.mod').exists():
        module, package = (source, '.')
    target = bins / binary
    execute(['go', 'build', '-o', target, package], module, os.environ.copy(), show=False)
    return str(target)

def shim(bins, name, body):
    target = bins / name
    target.write_text('#!/usr/bin/env python3\n' + body)
    target.chmod(493)

def demo(work, env, bins):
    binary = build('./extras/' + NAME, META['binary'], bins)
    if NAME == 'git':
        before = work / 'before.go'
        before.write_text(OLD)
        diff = execute([binary, before, work / 'internal/auth/token.go', 'internal/auth/token.go', 'always'], work, env, check=False)
        if diff.returncode != 1:
            raise RuntimeError('expected the token parser diff')
        return
    if NAME == 'desktop':
        report = work / 'review.md'
        report.write_text('# Token parser review\n\nReject misplaced prefixes before session lookup.\nKeep the empty-token regression test.\n')
        env['EDITOR'] = 'cat'
        env['VISUAL'] = 'cat'
        execute([binary, '--action', 'document', '--path', report], work, env)
        return
    print('Offline repair transcript fixture. Test output is captured from this fixture run.', flush=True)
    tests = execute(['go', 'test', '-v', './internal/auth'], work, env)
    stamp = '2026-09-09T10:00:00Z'
    home = pathlib.Path(env['HOME'])
    if NAME == 'codex':
        path = home / '.codex/sessions/2026/09/09/token-review.jsonl'
        path.parent.mkdir(parents=True)
        frames = [{'type': 'session_meta', 'timestamp': stamp, 'payload': {'id': 'token-review', 'cwd': str(work)}}, {'type': 'turn_context', 'timestamp': stamp, 'payload': {'turn_id': 'validate-token', 'cwd': str(work), 'model': 'recorded-review'}}, {'type': 'event_msg', 'timestamp': stamp, 'payload': {'type': 'task_started', 'turn_id': 'validate-token', 'trace_id': 'token-review'}}, {'type': 'event_msg', 'timestamp': stamp, 'payload': {'type': 'item_completed', 'thread_id': 'token-review', 'turn_id': 'validate-token', 'item': {'type': 'CommandExecution', 'id': 'token-tests', 'command': ['go', 'test', '-v', './internal/auth'], 'cwd': str(work), 'stdout': tests.stdout, 'exit_code': tests.returncode}}}, {'type': 'event_msg', 'timestamp': stamp, 'payload': {'type': 'task_complete', 'turn_id': 'validate-token'}}]
        path.write_text('\n'.join((json.dumps(frame) for frame in frames)) + '\n')
    elif NAME in {'claude', 'worklog'}:
        path = home / '.claude/projects/checkout-service/token-review.jsonl'
        path.parent.mkdir(parents=True)
        frames = [{'type': 'user', 'uuid': 'review-request', 'sessionId': 'token-review', 'cwd': str(work), 'timestamp': stamp, 'message': {'content': 'Check malformed authorization headers'}}, {'type': 'assistant', 'uuid': 'review-response', 'parentUuid': 'review-request', 'sessionId': 'token-review', 'requestId': 'token-check', 'timestamp': stamp, 'message': {'model': 'recorded-review', 'stop_reason': 'tool_use', 'content': [{'type': 'tool_use', 'id': 'token-tests', 'name': 'Bash', 'input': {'command': 'go test -v ./internal/auth'}}]}}, {'type': 'user', 'uuid': 'test-result', 'parentUuid': 'review-response', 'sessionId': 'token-review', 'timestamp': stamp, 'message': {'content': [{'type': 'tool_result', 'tool_use_id': 'token-tests', 'content': tests.stdout}]}}]
        path.write_text('\n'.join((json.dumps(frame) for frame in frames)) + '\n')
    elif NAME == 'antigravity':
        path = home / '.gemini/antigravity-cli/brain/token-review/.system_generated/logs/transcript_full.jsonl'
        path.parent.mkdir(parents=True)
        frames = [
            {'step_index': 0, 'source': 'USER_EXPLICIT', 'type': 'USER_INPUT', 'status': 'DONE', 'created_at': stamp, 'content': '<USER_REQUEST>Check malformed authorization headers</USER_REQUEST>'},
            {'step_index': 1, 'source': 'MODEL', 'type': 'PLANNER_RESPONSE', 'status': 'DONE', 'created_at': '2026-09-09T10:00:01Z', 'tool_calls': [{'name': 'run_command', 'args': {'CommandLine': 'go test -v ./internal/auth', 'Cwd': str(work), 'WaitMsBeforeAsync': 2000, 'toolSummary': 'Check token boundaries'}}]},
            {'step_index': 2, 'source': 'MODEL', 'type': 'GENERIC', 'status': 'DONE', 'created_at': '2026-09-09T10:00:02Z', 'content': 'Created At: 2026-09-09T10:00:01Z\nThe command exited with code ' + str(tests.returncode) + '.\nOutput:\n' + tests.stdout},
            {'step_index': 3, 'source': 'MODEL', 'type': 'PLANNER_RESPONSE', 'status': 'DONE', 'created_at': '2026-09-09T10:00:03Z', 'content': 'Reject misplaced prefixes before session lookup.'},
        ]
        path.write_text('\n'.join(json.dumps(frame) for frame in frames) + '\n')
    elif NAME == 'cursor':
        path = home / '.cursor/projects/checkout-service/agent-transcripts/token-review/token-review.jsonl'
        path.parent.mkdir(parents=True)
        frames = [
            {'role': 'user', 'message': {'content': [{'type': 'text', 'text': '<timestamp>Wednesday, Sep 9, 2026, 10:00 AM (UTC)</timestamp>\n<user_query>Check malformed authorization headers</user_query>'}]}},
            {'role': 'assistant', 'message': {'content': [{'type': 'tool_use', 'name': 'Shell', 'input': {'command': 'go test -v ./internal/auth', 'description': 'Check token boundaries'}}]}},
            {'role': 'assistant', 'message': {'content': [{'type': 'text', 'text': 'Reject misplaced prefixes before session lookup.'}]}},
            {'type': 'turn_ended', 'status': 'success'},
        ]
        path.write_text('\n'.join(json.dumps(frame) for frame in frames) + '\n')
        (path.parents[2] / '.workspace-trusted').write_text(json.dumps({'trustedAt': stamp, 'workspacePath': str(work), 'trustMethod': 'cli-flag'}))
    elif NAME == 'devin':
        import sqlite3
        path = pathlib.Path(env['XDG_DATA_HOME']) / 'devin/cli/sessions.db'
        path.parent.mkdir(parents=True)
        with sqlite3.connect(path) as database:
            database.execute('CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT, model TEXT, last_activity_at INTEGER, title TEXT, main_chain_id INTEGER, hidden INTEGER DEFAULT 0)')
            database.execute('CREATE TABLE message_nodes (session_id TEXT, node_id INTEGER, parent_node_id INTEGER, chat_message TEXT)')
            database.execute('INSERT INTO sessions VALUES (?,?,?,?,?,?,?)', ('token-review', str(work), 'offline-review', 1788948000, 'Check authorization headers', 3, 0))
            frames = [
                {'role': 'user', 'content': 'Check malformed authorization headers', 'metadata': {'is_user_input': True, 'created_at': stamp}},
                {'role': 'assistant', 'content': '', 'tool_calls': [{'id': 'token-tests', 'name': 'exec', 'index': 0, 'arguments': {'command': 'go test -v ./internal/auth'}}], 'metadata': {'request_id': 'token-check', 'finish_reason': 'tool_calls', 'created_at': '2026-09-09T10:00:01Z'}},
                {'role': 'tool', 'content': tests.stdout, 'tool_call_id': 'token-tests', 'metadata': {'created_at': '2026-09-09T10:00:02Z'}},
                {'role': 'assistant', 'content': 'Reject misplaced prefixes before session lookup.', 'metadata': {'request_id': 'token-result', 'created_at': '2026-09-09T10:00:03Z'}},
            ]
            for index, frame in enumerate(frames):
                database.execute('INSERT INTO message_nodes VALUES (?,?,?,?)', ('token-review', index, index - 1 if index else None, json.dumps(frame)))
    elif NAME == 'gate':
        path = pathlib.Path(env['XDG_STATE_HOME']) / 'gate/decisions.jsonl'
        path.parent.mkdir(parents=True)
        path.write_text(json.dumps({'ts': stamp, 'harness': 'claude', 'event': 'PreToolUse', 'tool': 'Read', 'session': 'token-review', 'cwd': str(work), 'final': 'deny', 'ms': 2, 'decisions': [{'provider': 'read-router', 'kind': 'deny', 'ms': 2, 'message': 'Read internal/auth/token.go with an offset and limit.'}]}) + '\n')
    elif NAME == 'opencode':
        doc = {'info': {'id': 'token-review', 'directory': str(work), 'time': {'created': 1000, 'updated': 9000}}, 'messages': [{'info': {'id': 'request', 'role': 'user', 'time': {'created': 1000}}, 'parts': [{'id': 'request-text', 'type': 'text', 'text': 'Check malformed authorization headers'}]}, {'info': {'id': 'review', 'role': 'assistant', 'finish': 'stop', 'time': {'created': 2000, 'completed': 8000}}, 'parts': [{'id': 'review-text', 'type': 'text', 'text': 'Reject misplaced prefixes before session lookup.'}]}]}
        shim(bins, 'opencode', "import json,sys\nif sys.argv[1:2] == ['db']:\n print(json.dumps([{'id':'token-review'}])); raise SystemExit(0)\nif sys.argv[1:2] != ['export']: raise SystemExit(2)\nprint(json.dumps(" + repr(doc) + '))\n')
    if NAME == 'worklog':
        output = work / 'worklog.jsonl'
        execute([binary, '--output', output, '--now', '2026-09-09T10:05:00Z'], work, env, json.dumps({'session_id': 'token-review', 'cwd': str(work), 'reason': 'clear', 'transcript_path': str(path)}))
        record = json.loads(output.read_text())
        print('Worklog: ' + record['first_prompt'], flush=True)
        print(str(record['duration_min']) + ' minutes in the transcript fixture', flush=True)
        for repository in record['repos']:
            print(repository['name'] + ': ' + repository['dirty'], flush=True)
        return
    data = execute([binary, '--exact-session', 'token-review'], work, env, show=False)
    traces = build('.', 'traces', bins)
    rendered = execute([traces, '--file', '-', '--once', '--color', 'always'], work, env, data.stdout, check=False)
    expected = 2 if NAME == 'gate' else 0
    if rendered.returncode != expected:
        raise RuntimeError('unexpected trace status: ' + str(rendered.returncode))

def main():
    with tempfile.TemporaryDirectory(prefix='token-review-') as temporary:
        root = pathlib.Path(temporary).resolve()
        work = root / 'checkout-service'
        bins = root / 'bin'
        bins.mkdir()
        (work / 'internal/auth').mkdir(parents=True)
        (work / 'go.mod').write_text('module checkout-service\n\ngo 1.26\n')
        path = work / 'internal/auth/token.go'
        path.write_text(OLD)
        (work / 'internal/auth/token_test.go').write_text(TEST)
        env = os.environ.copy()
        for name, dirname in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data'), ('XDG_STATE_HOME', 'state'), ('XDG_CACHE_HOME', 'cache'), ('XDG_RUNTIME_DIR', 'runtime')]:
            (root / dirname).mkdir()
            env[name] = str(root / dirname)
        env['PATH'] = str(bins) + os.pathsep + env['PATH']
        env['XDG_DATA_DIRS'] = str(root / 'data')
        for name in ['ORC_SESSION_ID', 'ORC_SCOPE', 'WEZTERM_PANE', 'WEZTERM_UNIX_SOCKET', 'GATE_STATE_DIR']:
            env.pop(name, None)
        execute(['git', 'init', '-b', 'main'], work, env, show=False)
        execute(['git', 'config', 'user.name', 'Review fixture'], work, env, show=False)
        execute(['git', 'config', 'user.email', 'review@example.invalid'], work, env, show=False)
        execute(['git', 'add', '.'], work, env, show=False)
        execute(['git', 'commit', '-m', 'add token parser'], work, env, show=False)
        path.write_text(NEW)
        print(META['summary'] + '\n', flush=True)
        demo(work, env, bins)
        print('\nDemo complete', flush=True)
if __name__ == '__main__':
    main()
