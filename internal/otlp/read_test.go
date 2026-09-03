package otlp

import "testing"

func TestSessionIDAcceptsGenericTelemetryAliases(t *testing.T) {
	tests := []string{
		"session.id",
		"conversation.id",
		"thread_id",
		"ai.telemetry.metadata.sessionId",
	}
	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			if got := SessionID(map[string]string{key: "run-123"}); got != "run-123" {
				t.Fatalf("SessionID() = %q", got)
			}
		})
	}
}

func TestSessionIDUsesStableAliasPrecedence(t *testing.T) {
	attrs := map[string]string{
		"session.id":      "standard",
		"conversation.id": "conversation",
		"thread_id":       "thread",
	}
	if got := SessionID(attrs); got != "standard" {
		t.Fatalf("SessionID() = %q, want standard", got)
	}
}

func TestDecodeLogAcceptsTypedBodyAndAttributes(t *testing.T) {
	input := `{
  "resourceLogs": [{
    "resource": {
      "attributes": [{"key": "conversation.id", "value": {"stringValue": "run-123"}}]
    },
    "scopeLogs": [{
      "logRecords": [{
        "timeUnixNano": "1000000000",
        "eventName": "count",
        "body": {"intValue": "42"},
        "attributes": [{"key": "sampled", "value": {"boolValue": true}}]
      }]
    }]
  }]
}`
	batch := Decode([]byte(input))
	if len(batch.Records) != 1 {
		t.Fatalf("records = %+v", batch.Records)
	}
	record := batch.Records[0]
	if record.Body != "42" || record.Session != "run-123" || record.Attrs["sampled"] != "true" {
		t.Fatalf("record = %+v", record)
	}
}
