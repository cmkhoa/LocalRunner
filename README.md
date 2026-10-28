# LocalRunner

LocalRunner is a small Go service that manages one local `llama.cpp` runner and
exposes it through a localhost HTTP API. It owns process lifecycle, health,
request queueing, and metrics; `llama.cpp` performs inference.

## Current progress

The MVP and its initial stretch features are complete. LocalRunner can manage a
local GGUF runner, queue regular or streamed chat requests, report operational
metrics, reuse model profiles, and optionally recover from a child-process exit.

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

Build both the server and terminal client with:

```sh
make build
```

## CLI client

`localrunnerctl` wraps the localhost API for the common workflow. Run it without
installing anything else:

```sh
go run ./cmd/localrunnerctl status
go run ./cmd/localrunnerctl models
go run ./cmd/localrunnerctl profiles
go run ./cmd/localrunnerctl activate qwen3-local
go run ./cmd/localrunnerctl start
go run ./cmd/localrunnerctl chat "Explain LocalRunner in one sentence."
go run ./cmd/localrunnerctl metrics
go run ./cmd/localrunnerctl stop
```

Use `--stream` to print raw server-sent events and `--address` (or
`LOCALRUNNER_API_URL`) to target a non-default local API address:

```sh
go run ./cmd/localrunnerctl --address http://127.0.0.1:8183 chat --stream "Count to three."
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
| `LOCALRUNNER_AUTO_RESTART` | `false` | Opt in to automatic recovery after an unexpected child exit |
| `LOCALRUNNER_AUTO_RESTART_DELAY` | `2s` | Delay before an automatic restart attempt |
| `LOCALRUNNER_MAX_AUTO_RESTARTS` | `3` | Maximum automatic attempts per LocalRunner process |
| `LOCALRUNNER_MAX_REQUEST_BYTES` | `1048576` | Largest accepted JSON request body |
| `LOCALRUNNER_MAX_INPUT_BYTES` | `32768` | Combined message-content byte limit |
| `LOCALRUNNER_MAX_GENERATED_TOKENS` | `512` | Largest accepted `max_tokens` value |
| `LOCALRUNNER_DEFAULT_MAX_TOKENS` | `256` | Generation limit when `max_tokens` is omitted |
| `LOCALRUNNER_QUEUE_CAPACITY` | `16` | Maximum queued chat requests before HTTP 429 |
| `LOCALRUNNER_REQUEST_TIMEOUT` | `2m` | End-to-end chat-completion time limit |

Automatic restart is intentionally disabled by default. When enabled, an
unexpected runner exit first becomes observable as `unhealthy`; LocalRunner then
waits for the configured delay and retries. It stops scheduling attempts after
the configured maximum, avoiding an unbounded crash loop. Explicit stop requests
never schedule a restart.

When a start attempt or child process fails, `GET /v1/runners/status` includes
a bounded `last_error` diagnostic. It excludes prompt and response data and is
cleared after the runner becomes healthy or is deliberately stopped.

### Named model profiles

For repeatable settings, set `LOCALRUNNER_PROFILES_PATH` to a JSON file shaped
like [configs/profiles.example.json](configs/profiles.example.json). Profiles
have a name, validated local GGUF path, context size, and requested GPU layers;
they do not accept arbitrary command-line fragments.

```sh
LOCALRUNNER_PROFILES_PATH=/absolute/path/to/profiles.json go run ./cmd/localrunner

curl http://127.0.0.1:8080/v1/models/profiles
curl -X POST http://127.0.0.1:8080/v1/models/activate \
  -H 'Content-Type: application/json' \
  -d '{"name":"qwen3-4b"}'
curl -X POST http://127.0.0.1:8080/v1/runners/start
```

Profiles may only be activated while the runner is stopped or unhealthy. Direct
`/v1/models/register` remains available for one-off model paths.

For OpenAI-style client discovery, `GET /v1/models` exposes the active model
and configured profile names as `object: "model"` records. It deliberately does
not expose local GGUF paths.

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
supported through standard server-sent events (SSE). Set `"stream": true` in
the same request shape to receive incremental `data:` chunks and a final
`data: [DONE]` event. Streaming requests retain the queue worker until the
stream ends, so they cannot overload the active model.

```sh
curl --no-buffer http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": [{"role":"user","content":"Count from one to three."}],
    "stream": true,
    "chat_template_kwargs": {"enable_thinking": false}
  }'
```

## Sample runs

The following were run through LocalRunner with the local `qwen3-local` profile
on this machine, using Qwen thinking disabled and temperature 0.

### Standard completion

Prompt: `In exactly one sentence, explain what a process supervisor does.`

Observed response:

> A process supervisor oversees and coordinates the operations of a process to
> ensure it runs efficiently, meets objectives, and adheres to safety and
> quality standards.

The request generated 30 tokens at 41.36 tokens/sec according to `llama.cpp`.

### Streamed completion

Prompt: `Reply with exactly: streaming is live`

Observed SSE content chunks:

```text
stream
ing
 is
 live
[DONE]
```

Together, those chunks produce `streaming is live`. The final SSE event reported
5 generated tokens at 40.08 tokens/sec.

## Metrics

`GET /metrics` returns process-lifetime JSON aggregates without retaining
prompts or responses. It includes successful/failed request counts and error
categories, total and average queue waiting time, total and average end-to-end
latency, generated-token totals, and the average generation rate reported by
`llama.cpp` for successful regular and streamed requests. Metrics reset whenever
LocalRunner restarts.

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
