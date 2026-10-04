# LocalRunner — Product and Delivery Roadmap

## 1. Product definition

### Problem

Running a local GGUF model through `llama.cpp` is straightforward for a one-off
terminal session, but the process is awkward to operate from another local
application. Callers need a stable endpoint, a clear readiness signal, safe
process shutdown, protection from concurrent overload, and enough telemetry to
understand whether the model is working well.

### Product promise

LocalRunner turns one local `llama.cpp` server and one explicitly selected GGUF
file into a dependable localhost service.

It owns lifecycle and operations; `llama.cpp` owns inference.

### Primary user

A developer running a local application, script, or agent on their own machine
who wants to call one local model via HTTP without hand-managing the underlying
`llama.cpp` process.

### First successful user journey

1. Configure the LocalRunner binary with the path to `llama-server`.
2. Register a local `.gguf` file by absolute path.
3. Start the runner and wait until its status is `ready`.
4. Send a non-streaming chat request to LocalRunner.
5. Receive the model response from LocalRunner.
6. Inspect status and metrics.
7. Stop the runner; verify no child process remains.

## 2. Product boundaries

### In the initial release

- Exactly one active GGUF model at a time.
- Localhost-only Go HTTP server.
- Explicit, validated filesystem paths for the model and `llama-server` binary.
- Managed `llama.cpp` subprocess lifecycle.
- Non-streaming chat completion proxy.
- One-worker FIFO request queue.
- Runner health, structured logs, and in-memory operational metrics.
- Unit tests plus a mock-runner integration test.

### Deliberately out of scope

- An inference engine, model downloads, registries, or model-file storage.
- Internet-facing access, authentication, users, or tenancy.
- GPU backend implementation work.
- Multiple concurrently loaded models or automatic model selection.
- Persistent prompts, responses, conversations, or metrics history.
- Streaming, embeddings, tool calling, and browser UI.
- Claiming OpenAI or Ollama compatibility before it is implemented and tested.

## 3. Architecture and interface decisions

```text
Local client
     |
     v
LocalRunner HTTP API (127.0.0.1)
  ├─ registry/configuration
  ├─ runner manager + state machine
  ├─ single-worker FIFO queue
  ├─ request proxy and validation
  └─ metrics + structured logging
     |
     v
llama.cpp `llama-server` subprocess
     |
     v
active local GGUF model
```

### Initial runner states

| State | Meaning | Valid next states |
| --- | --- | --- |
| `stopped` | No runner process is active. | `starting` |
| `starting` | Process launched; readiness check is pending. | `ready`, `unhealthy`, `stopping` |
| `ready` | Process is healthy and can receive queued work. | `unhealthy`, `stopping` |
| `unhealthy` | Process exited or failed health checks. | `stopping`, `starting`, `stopped` |
| `stopping` | Graceful termination is in progress. | `stopped`, `unhealthy` |

### API contract, v1

| Method | Path | Success | Key failure cases |
| --- | --- | --- | --- |
| `POST` | `/v1/models/register` | Active model recorded | missing/non-GGUF/unreadable path; runner active |
| `POST` | `/v1/runners/start` | Start accepted or runner ready | no registered model; invalid executable; already transitioning |
| `POST` | `/v1/runners/stop` | Stop completed | not running; shutdown timeout |
| `GET` | `/v1/runners/status` | State, PID, model, health | n/a |
| `POST` | `/v1/chat/completions` | Non-streaming model response | invalid request; runner unavailable; queue/request timeout; upstream failure |
| `GET` | `/metrics` | JSON operational snapshot | n/a |

All errors use one JSON shape: `{"error":{"code":"...","message":"...","request_id":"..."}}`.

### Safety and privacy rules

- Bind only to `127.0.0.1` by default; remote binding is not part of v1.
- Never construct a shell command from API input. Launch the executable directly
  with a fixed argument list.
- Canonicalize and validate paths before registration; accept only readable,
  regular `.gguf` files.
- Apply limits to request body size, input size, generated tokens, queue wait,
  and upstream request duration.
- Do not write prompts or generated text to logs, metrics, or disk by default.
- On stop, send graceful termination, wait for a bounded interval, then force
  termination only if necessary; always reap the child process.

## 4. Delivery sequence

Each phase has a demonstrable result. Do not begin a later phase until the
previous phase's exit criteria are met or consciously waived.

### Phase 0 — Technical validation and project foundations

**Goal:** prove the machine can serve a chosen small GGUF model through
`llama.cpp`, then establish a reproducible Go project.

**Work**

- Select a 1B–4B quantized GGUF model available on the development machine.
- Locate/build a known `llama-server` executable and run it manually once.
- Record executable version, model name/quantization, hardware/device, exact
  command, and known health/generation endpoints.
- Create the Go module, directory layout, configuration parsing, logger, and
  test/lint commands.
- Define API types, error shape, configuration defaults, and runner-state
  transition table.

**Exit criteria**

- Manual `llama-server` generation succeeds with the selected model.
- `go test ./...` runs successfully on the empty/service skeleton.
- `GET /v1/runners/status` returns valid JSON with `stopped`.

**Risk gate:** If the local model cannot run reliably, use a mock runner for
development but do not claim real-model performance or completion readiness.

### Phase 1 — Model registration and runner lifecycle

**Goal:** safely own one `llama.cpp` process from start through cleanup.

**Work**

- Implement configuration for the executable path, bind address, runner port,
  readiness timeout, shutdown timeout, and conservative request limits.
- Implement model registration and path validation.
- Build the runner manager around `os/exec`, explicit arguments, process
  tracking, stdout/stderr capture, and synchronization.
- Implement readiness polling against the verified `llama.cpp` endpoint.
- Implement start, stop, status, failed-start cleanup, unexpected-exit handling,
  and restart counting.

**Exit criteria**

- Register → start → ready → stop succeeds twice consecutively.
- Status includes state, PID when applicable, model identity/path policy, and
  last health result.
- Failed launch and readiness timeout leave no orphaned child process.
- Unit tests cover state transitions and path validation.

### Phase 2 — Controlled request proxy

**Goal:** serve one correct non-streaming chat request without overloading the
runner.

**Work**

- Implement the request/response types after confirming the target `llama.cpp`
  server's supported chat endpoint and payload.
- Validate body size, messages/input, maximum output tokens, and caller timeout.
- Implement a cancellable single-worker FIFO queue.
- Proxy queue jobs to the active runner with a bounded upstream timeout.
- Return upstream responses with a documented LocalRunner error mapping.
- Add request IDs to responses and structured operational logs.

**Exit criteria**

- A valid request returns generated text through LocalRunner.
- Two simultaneous requests complete in submission order.
- An invalid request never reaches the runner.
- Requests fail predictably while stopped, starting, unhealthy, stopping, or
  after timeout.

### Phase 3 — Observability and recovery evidence

**Goal:** make runtime behavior measurable and failures diagnosable.

**Work**

- Record request totals, completed/failed counts by category, queue wait,
  end-to-end latency, generated-token totals, tokens/second when supplied by
  the runner, and runner restart count.
- Implement JSON `/metrics` with a stable schema and clearly define resets
  (process lifetime only for v1).
- Ensure logs are structured and exclude prompts/responses by default.
- Add mock-runner integration tests for successful proxying, upstream timeout,
  upstream error, and health failure.
- Kill the child process in an integration/manual test and verify `unhealthy`.

**Exit criteria**

- Metrics change correctly after successful and failed requests.
- A killed runner becomes observable as `unhealthy`; subsequent requests return
  the runner-unavailable error.
- `go test ./...` passes without requiring a model download.

### Phase 4 — Release polish and reproducibility

**Goal:** enable a reviewer to understand and reproduce the MVP quickly.

**Work**

- Write README sections for problem, non-goals, architecture, prerequisites,
  configuration, API examples, operational behavior, troubleshooting, and
  known limitations.
- Add reproducible `make` targets or documented commands for format, test, run,
  and demo.
- Run 10–20 comparable requests and publish factual, machine-specific results.
- Capture a short terminal demo: register → start → chat → metrics → stop.
- Add CI for formatting and tests if repository hosting is configured.

**Exit criteria**

- A new reviewer can complete the primary journey from the README.
- Benchmark results name model, quantization, hardware, context/output settings,
  sample size, and what latency statistic is reported.
- No models, secrets, or local-only configuration are committed.

## 5. Work breakdown and dependency order

```text
P0 validate llama.cpp + model
 └─ project skeleton, API types, state model
     └─ P1 registration + process manager + readiness + shutdown
         └─ P2 queue + validation + HTTP proxy
             └─ P3 metrics + failure/recovery integration tests
                 └─ P4 docs + demo + benchmark + optional CI
```

Within Phase 1, state transitions and process cleanup precede the public start
and stop handlers. Within Phase 2, request validation precedes queueing, and
queueing precedes upstream proxying. This sequence avoids building an API around
unverified runner behavior.

## 6. Backlog after the MVP

Only consider these once all Phase 4 exit criteria pass.

| Priority | Capability | Reason | Prerequisite |
| --- | --- | --- | --- |
| P1 | Server-sent-event streaming | Better interactive chat UX | cancellation semantics and streamed metrics |
| P2 | OpenAI-shaped chat compatibility | Easier local-client adoption | contract tests against documented subset |
| P3 | Named model profiles | Repeatable runtime options | profile validation and lifecycle ownership |
| P4 | Automatic restart | Better resilience | backoff, loop prevention, clear status semantics |
| P5 | CLI client | Faster local workflow | stable API and config format |
| P6 | Embeddings/tool calling/multi-models | Broader capability | separate product design; not an incremental toggle |

## 7. Risks and decisions to resolve early

| Risk or decision | Why it matters | Mitigation / decision rule |
| --- | --- | --- |
| `llama.cpp` endpoint/version differences | Proxy and health contracts vary by build | Pin and document the tested version; isolate upstream client code. |
| Model does not fit or performs poorly | Blocks credible local validation | Start with a small quantized model; document actual hardware and settings. |
| Runner port collision | Start can target the wrong process or fail | Configure a dedicated port; verify process ownership and readiness. |
| Stop races with queued work | Can strand callers or leak processes | Reject new work while stopping; cancel/drain queued jobs with a defined error. |
| FIFO queue grows unbounded | Local callers can exhaust memory/time | Enforce maximum depth and return a clear overload error. |
| Token statistics differ upstream | Metrics may be unavailable/inconsistent | Make throughput optional; report unavailable rather than inventing it. |
| Scope creep toward Ollama parity | Endangers the MVP | Use this roadmap’s explicit cut line; promote only completed phases. |

## 8. MVP acceptance checklist

- [ ] A valid local GGUF file can be registered by explicit path.
- [ ] LocalRunner launches and health-checks the configured `llama-server`.
- [ ] Status exposes each runner state and active-model information.
- [ ] A non-streaming chat request reaches the model and returns a response.
- [ ] Concurrent requests are serialized FIFO, with bounded queue capacity.
- [ ] Invalid, unavailable, timed-out, and upstream-failure cases use consistent
      JSON errors.
- [ ] Metrics cover requests, failures, latency, queue waiting, and throughput
      when available.
- [ ] Stop/restart operations do not leave orphaned child processes.
- [ ] Unit tests and mock-runner integration tests pass via one documented command.
- [ ] README includes exact local setup and one honest benchmark result.

## 9. Suggested first implementation slice

Build only enough to reach this vertical slice:

1. Go service starts and exposes `GET /v1/runners/status` as `stopped`.
2. `POST /v1/models/register` accepts one validated GGUF path.
3. `POST /v1/runners/start` launches the configured `llama-server` and waits
   for health.
4. `POST /v1/runners/stop` reaps the process safely.

Do not add chat proxying before this lifecycle slice is repeatable. It is the
foundation that distinguishes LocalRunner from a thin HTTP forwarder.
