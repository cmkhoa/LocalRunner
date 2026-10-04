# LocalRunner

LocalRunner is a small Go service that manages one local `llama.cpp` runner and
exposes it through a localhost HTTP API. It owns process lifecycle, health,
request queueing, and metrics; `llama.cpp` performs inference.

## Current progress

Phase 2 is complete. LocalRunner can validate and register a GGUF path, start
one managed `llama-server` process, queue non-streaming chat requests FIFO, and
stop the runner cleanly.

## Run the foundation

```sh
go test ./...
go run ./cmd/localrunner
curl http://127.0.0.1:8080/v1/runners/status
```

Expected response:

```json
{"state":"stopped","healthy":false,"restart_count":0}
```

Set `LOCALRUNNER_LISTEN_ADDRESS` to another loopback `host:port` value if
needed. Non-loopback addresses are rejected by design.

For the standard development checks, run:

```sh
make verify
```

## Register and run a model

With the Homebrew-installed `llama-server` and the downloaded Qwen model:

```sh
go run ./cmd/localrunner

curl -X POST http://127.0.0.1:8080/v1/models/register \
  -H 'Content-Type: application/json' \
  -d '{"path":"/absolute/path/to/Qwen3-4B-Q4_K_M.gguf"}'

curl -X POST http://127.0.0.1:8080/v1/runners/start
curl http://127.0.0.1:8080/v1/runners/status
curl -X POST http://127.0.0.1:8080/v1/runners/stop
```

The service starts the runner on `127.0.0.1:8081` by default and waits for its
`/health` endpoint before returning `ready`. The runner uses a 2,048-token
context and requests 99 GPU layers by default, which enables full Metal
offload where the machine supports it.

Configuration is intentionally process-level, never supplied in HTTP requests:

| Variable | Default | Purpose |
| --- | --- | --- |
| `LOCALRUNNER_LISTEN_ADDRESS` | `127.0.0.1:8080` | LocalRunner API bind address |
| `LOCALRUNNER_LLAMA_SERVER_PATH` | `llama-server` | Executable found on `PATH` or an explicit path |
| `LOCALRUNNER_RUNNER_ADDRESS` | `127.0.0.1:8081` | Managed llama-server bind address |
| `LOCALRUNNER_CONTEXT_SIZE` | `2048` | Runner context-token limit |
| `LOCALRUNNER_GPU_LAYERS` | `99` | Requested GPU-offloaded layers |
| `LOCALRUNNER_READINESS_TIMEOUT` | `60s` | Maximum start/readiness interval |
| `LOCALRUNNER_SHUTDOWN_TIMEOUT` | `10s` | Graceful-stop limit before forced termination |

## Chat completion

Once the runner is `ready`, submit a non-streaming request through LocalRunner:

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": [{"role":"user","content":"Say hello in one sentence."}],
    "max_tokens": 64,
    "chat_template_kwargs": {"enable_thinking": false}
  }'
```

Requests are serialized through one worker to protect the active model. The
queue holds at most 16 waiting requests; an overflow returns HTTP 429. Requests
must contain 1–64 `system`, `user`, or `assistant` messages, at most 32 KiB of
message text, and no more than 512 generated tokens. Streaming is intentionally
not implemented in this release.

## Metrics

`GET /metrics` returns process-lifetime JSON aggregates without retaining
prompts or responses. It includes successful/failed request counts and error
categories, total and average queue waiting time, total and average end-to-end
latency, generated-token totals, and the average generation rate reported by
`llama.cpp` for successful requests. Metrics reset whenever LocalRunner restarts.

Every response also includes an `X-Request-ID` header. JSON error bodies repeat
that value as `error.request_id`; the same ID is written to completion logs with
operational fields only (never prompt or generated text).

## Measured result

On this Apple Silicon (`arm64`) Mac running macOS 27.0.1, a sequential benchmark
of 10 identical requests completed with no failures. The configuration was
`Qwen3-4B-Q4_K_M.gguf`, `llama.cpp` 0.5.0, 2,048 context tokens, 99 requested
GPU layers, temperature 0, a 16-token cap, and Qwen thinking disabled. The
prompt was “Reply with exactly: benchmark complete”.

| Metric | Observed value |
| --- | ---: |
| Successful requests | 10 / 10 |
| Average end-to-end latency | 87.8 ms |
| Average queue wait | 0.006 ms |
| Generated tokens | 30 |
| Average `llama.cpp` generation rate | 41.0 tokens/sec |

These are machine- and prompt-specific observations, not general performance
claims.

The same machine also completed a recovery check: after terminating the managed
`llama-server` child, LocalRunner changed status to `unhealthy`, started a new
child successfully, and reported a restart count of 1.

## Local inference prerequisite

Install/build a compatible `llama.cpp` `llama-server` executable and have a
local GGUF model available. LocalRunner requires an explicit model path; it
will not download models or accept shell command strings from HTTP requests.

See [ROADMAP.md](ROADMAP.md) for the planned delivery phases and scope.
