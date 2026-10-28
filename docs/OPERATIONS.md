# Operations and Configuration

LocalRunner is process-level configuration: settings are supplied at startup,
not through HTTP. Restart the service after changing them.

| Variable | Default | Purpose |
| --- | --- | --- |
| `LOCALRUNNER_LISTEN_ADDRESS` | `127.0.0.1:8080` | LocalRunner API bind address |
| `LOCALRUNNER_LLAMA_SERVER_PATH` | `llama-server` | Runner executable path or command |
| `LOCALRUNNER_RUNNER_ADDRESS` | `127.0.0.1:8081` | Managed runner bind address |
| `LOCALRUNNER_CONTEXT_SIZE` | `2048` | Runner context-token limit |
| `LOCALRUNNER_GPU_LAYERS` | `99` | Requested GPU-offloaded layers |
| `LOCALRUNNER_READINESS_TIMEOUT` | `60s` | Start/readiness deadline |
| `LOCALRUNNER_SHUTDOWN_TIMEOUT` | `10s` | Graceful-stop deadline |
| `LOCALRUNNER_AUTO_RESTART` | `false` | Enable bounded restart after unexpected exit |
| `LOCALRUNNER_AUTO_RESTART_DELAY` | `2s` | Delay before a restart attempt |
| `LOCALRUNNER_MAX_AUTO_RESTARTS` | `3` | Restart-attempt budget |
| `LOCALRUNNER_MAX_REQUEST_BYTES` | `1048576` | Largest JSON request body |
| `LOCALRUNNER_MAX_INPUT_BYTES` | `32768` | Combined message-content size |
| `LOCALRUNNER_MAX_GENERATED_TOKENS` | `512` | Largest accepted `max_tokens` |
| `LOCALRUNNER_DEFAULT_MAX_TOKENS` | `256` | Output limit when omitted |
| `LOCALRUNNER_QUEUE_CAPACITY` | `16` | Waiting-request capacity before HTTP 429 |
| `LOCALRUNNER_REQUEST_TIMEOUT` | `2m` | End-to-end chat deadline |
| `LOCALRUNNER_PROFILES_PATH` | unset | Path to named-profile JSON |

The queue has one worker. Requests execute FIFO; canceled queued work is
skipped. A long-lived stream occupies that worker until it ends. Named profiles
may override `readiness_timeout`, `request_timeout`, `max_generated_tokens`,
`default_max_tokens`, and `queue_capacity`; omitted values inherit this table.

## Health, recovery, and diagnostics

`GET /v1/runners/status` reports `stopped`, `starting`, `ready`, `unhealthy`,
or `stopping`. Failed readiness checks and unexpected child exits produce a
bounded, sanitized `last_error`. It is cleared after a successful start or an
intentional stop.

Automatic restart is off by default. When enabled, an unexpected exit becomes
visible as `unhealthy`, then retries after the configured delay until its budget
is exhausted. Explicit stops never restart.

## Metrics and logs

`GET /metrics` reports process-lifetime request counts, error categories, queue
wait, latency, generated tokens, and generation rate. Metrics reset on service
restart. Structured logs contain operational fields and request IDs, never
prompts or generated text.

## Verify and build

```sh
make verify
make build
```

The initial measured Qwen run on this machine completed 10 sequential requests
with no failures, averaging 87.8 ms end-to-end and 41.0 generated tokens/sec.
This is a machine-specific observation, not a general performance claim.
