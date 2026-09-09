#!/usr/bin/env python3
"""Record local test output in an explicitly synthetic transcript envelope."""
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import sys
work = Path(sys.argv[1]).resolve()
output = Path(sys.argv[2])
stamp = datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')
tests = subprocess.run(['go', 'test', '-v', './internal/auth'], cwd=work, text=True, capture_output=True, check=True)
frames = [
 {'type':'session_meta','timestamp':stamp,'payload':{'id':'token-review','cwd':str(work)}},
 {'type':'turn_context','timestamp':stamp,'payload':{'turn_id':'token-boundary','cwd':str(work),'model':'offline-review-fixture'}},
 {'type':'event_msg','timestamp':stamp,'payload':{'type':'task_started','turn_id':'token-boundary','trace_id':'token-review'}},
 {'type':'event_msg','timestamp':stamp,'payload':{'type':'item_completed','thread_id':'token-review','turn_id':'token-boundary','item':{'type':'CommandExecution','id':'token-tests','command':['go','test','-v','./internal/auth'],'cwd':str(work),'stdout':tests.stdout,'exit_code':tests.returncode}}},
 {'type':'event_msg','timestamp':stamp,'payload':{'type':'task_complete','turn_id':'token-boundary'}},
]
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(''.join(json.dumps(frame)+'\n' for frame in frames))
