def nanos: (fromdateiso8601 | tostring) + "000000000";
if .conclusion != "success" then error("record a completed successful run") else . end
| "orc-ci-\(.databaseId)" as $trace
| {
    traceId: $trace, spanId: "workflow", name: "Orc \(.name)",
    service: "GitHub Actions", session: $trace,
    startUnixNano: (.startedAt | nanos), endUnixNano: (.updatedAt | nanos),
    attrs: {url: .url, commit: .headSha, conclusion: .conclusion}
  },
  (.jobs[] | . as $job | "job-\(.databaseId)" as $job_id
   | {
       traceId: $trace, spanId: $job_id, parentId: "workflow", name: .name,
       service: "GitHub Actions", session: $trace,
       startUnixNano: (.startedAt | nanos), endUnixNano: (.completedAt | nanos),
       attrs: {url: .url, conclusion: .conclusion}
     },
     (.steps[] | select(.status == "completed" and .conclusion != "skipped")
      | {
          traceId: $trace, spanId: "\($job_id)-step-\(.number)", parentId: $job_id,
          name: .name, service: "GitHub Actions", session: $trace,
          startUnixNano: (.startedAt | nanos), endUnixNano: (.completedAt | nanos),
          attrs: {job: $job.name, conclusion: .conclusion, url: $job.url}
        }))
