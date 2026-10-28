# LocalRunner — Weekend MVP Plan

## Goal

Build a small, local LLM runner manager that starts and supervises a `llama.cpp` server process, exposes a single model through a clean HTTP API, and reports useful runtime health and performance information.

The project is **not** a new inference engine or a full Ollama replacement. `llama.cpp` performs inference; LocalRunner owns process lifecycle, request routing, API design, queuing, health, and observability.

## Definition of done

By Sunday night, a reviewer can:

1. Register one local GGUF model by an explicit file path.
2. Start a `llama.cpp` runner through LocalRunner.
3. Send a chat request to LocalRunner and receive a generated response.
4. Check whether the runner is healthy and whether a model is loaded.
5. View request count, failures, latency, and generated-token throughput.
6. Stop the runner cleanly and restart it without orphaned processes.

## Scope

### Build this weekend

- One local, quantized GGUF model at a time; begin with a small 1B–4B model.
- `llama.cpp` launched as a managed subprocess.
- A local REST API, bound only to `127.0.0.1`.
- A chat-completion endpoint that proxies to the active runner.
- A status endpoint and a small metrics endpoint.
- FIFO request queuing so concurrent requests do not overload a single runner.
- Structured logs and basic integration tests.
- A README with setup steps, architecture, a short demo, and measured results from this machine.

### Explicitly defer

- Writing a model-inference engine.
- GPU/CUDA/Metal implementation work.
- Multiple simultaneously loaded models.
- A public deployment, authentication, or multi-user tenancy.
- Model downloads, registries, or an Ollama-compatible model format.
- Streaming responses, embeddings, tool calling, and a web UI.

If the core API works early, streaming is the one optional enhancement worth considering.

## Architecture

```text
Client
  |
  v
LocalRunner API (Go)
  |- model registry: validated local GGUF path
  |- runner manager: start / stop / restart / health
  |- FIFO request queue
  |- metrics + structured logs
  |
  v
llama.cpp server subprocess
  |
  v
Local GGUF model
```

## Proposed API

Keep the API intentionally small.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/v1/models/register` | Validate and register a local GGUF file path. |
| `POST` | `/v1/runners/start` | Start the active model's runner. |
| `POST` | `/v1/runners/stop` | Gracefully stop the runner. |
| `GET` | `/v1/runners/status` | Return runner state, PID, active model, and health. |
| `POST` | `/v1/chat/completions` | Queue and proxy a non-streaming chat request. |
| `GET` | `/metrics` | Return request count, errors, latency, and throughput. |

Use a simple status model: `stopped`, `starting`, `ready`, `unhealthy`, and `stopping`.

## Data to record

For every request, record only operational data—never persist the prompt or generated response by default:

- request ID;
- timestamp;
- active model identifier;
- queue wait time;
- total request latency;
- generated-token count and tokens/second, if available;
- HTTP status / error category;
- runner restart count.

## Weekend schedule

### Friday evening — prove the dependency and scaffold the service

**Outcome:** One `llama.cpp` server can run a small local GGUF model manually, and the Go service skeleton starts.

- Install or build `llama.cpp` and run one small GGUF model locally.
- Capture the exact command, model name, device, and machine configuration for the README.
- Create the Go project with configuration, structured logging, and a health endpoint.
- Define request/response structs and the runner state machine.
- Add a `Makefile` or a few documented commands for run/test/lint.

**Checkpoint:** `GET /v1/runners/status` returns `stopped` with a valid JSON response.

### Saturday morning — runner lifecycle

**Outcome:** LocalRunner can register, start, health-check, and stop one model runner.

- Implement GGUF-path validation and model registration.
- Implement subprocess launch using an explicit `llama.cpp` executable path and arguments.
- Wait for the child process's health endpoint before marking the runner `ready`.
- Capture stdout/stderr in structured logs.
- Implement graceful shutdown, timeout handling, and protection against orphaned processes.

**Checkpoint:** Start → ready → stop works twice in a row without manual cleanup.

### Saturday afternoon — request proxy and queue

**Outcome:** A client can submit a chat request through LocalRunner.

- Implement `POST /v1/chat/completions`.
- Validate request limits: input size, maximum generated tokens, and timeout.
- Add a single-worker FIFO queue.
- Proxy requests to the `llama.cpp` server and return its generated response.
- Add request IDs and error categories: runner unavailable, timeout, invalid request, upstream failure.

**Checkpoint:** Two concurrent requests complete in order and return valid responses.

### Sunday morning — metrics and tests

**Outcome:** The service produces evidence that it behaves correctly.

- Add in-memory counters and latency/throughput aggregation.
- Implement `/metrics` as JSON; Prometheus format is optional.
- Write unit tests for state transitions, path validation, and queue ordering.
- Write an integration test that starts a mock runner or a lightweight real runner when available.
- Test failure recovery by killing the runner and verifying `unhealthy` status.

**Checkpoint:** `go test ./...` passes and the service reports meaningful metrics after requests.

### Sunday afternoon — polish and document

**Outcome:** The repository is understandable in five minutes.

- Write the README: problem, non-goals, architecture, setup, API examples, metrics, and limitations.
- Include a terminal screenshot or short GIF showing register → start → request → metrics → stop.
- Run a reproducible benchmark of 10–20 identical/similar requests and report observed results without overgeneralizing.
- Add a GitHub Actions workflow for formatting and tests, if time permits.
- Clean up errors, names, and setup steps from a fresh terminal.

## Recommended repository structure

```text
localrunner/
├── cmd/localrunner/        # application entry point
├── internal/
│   ├── api/                # HTTP handlers and request validation
│   ├── runner/             # subprocess lifecycle and state machine
│   ├── queue/              # FIFO job queue
│   └── metrics/            # request and runner metrics
├── configs/                # example local configuration only
├── tests/                  # integration tests / mock runner
├── README.md
└── Makefile
```

## Quality bar

- No secret keys or model files committed.
- Model paths are explicit and validated; do not accept arbitrary shell strings from requests.
- Child processes always receive a bounded shutdown timeout.
- Every response uses a consistent JSON error shape.
- Do not claim OpenAI or Ollama compatibility unless you implement and test it.
- README results are labeled with the exact model, quantization, hardware, and prompt settings used.

## Stretch goals, in priority order

1. Server-sent-event streaming for chat responses.
2. OpenAI-compatible request/response shapes for the chat endpoint.
3. Configurable model profiles (path, context size, runtime arguments).
4. Automatic restart after an unhealthy runner process.
5. A small CLI client.

## Post-MVP expansion roadmap

The product should grow into a dependable local AI runtime and control plane,
not a replacement inference engine. `llama.cpp` remains the first inference
backend; LocalRunner's value is a consistent developer experience around local
models.

### 1. Runtime hardening

**Goal:** make the existing local service trustworthy during long-running use.

- Add automated integration tests using a mock or lightweight real runner.
- Support cancellation for queued requests and active streaming responses.
- Make queue depth, request timeout, startup timeout, and output-token limits
  configurable per profile.
- Add durable structured-log output and clearer child-process crash diagnostics.
- Record generated-token and throughput metrics for streamed responses as well
  as regular completions.

**Exit criteria:** canceled work does not linger in the queue or runner; a
failure can be diagnosed from status, metrics, and logs without recording a
prompt or response.

### 2. OpenAI compatibility

**Goal:** let existing local AI tools use LocalRunner with minimal adaptation.

- Add `GET /v1/models` for the active model and configured profiles.
- Define and test the supported OpenAI-compatible subset of chat-completion
  request and response fields.
- Add an optional localhost API key for tools that require one, without
  implying public deployment or multi-user support.
- Add a machine-readable/JSON-output mode to `localrunnerctl`.

**Exit criteria:** a documented set of OpenAI-style clients can list the active
model and make regular or streaming chat requests without custom adapters.

### 3. Model management and compatibility checks

**Goal:** make model selection repeatable and fail early on bad configurations.

- Discover and validate configured profiles at startup.
- Inspect GGUF metadata before launch: architecture, tokenizer/template hints,
  model size, and quantization details where available.
- Estimate memory/context requirements and recommend safe runtime settings for
  the current machine.
- Add opt-in model import/download workflows with explicit storage locations.
- Persist per-profile benchmark history with exact machine and runtime settings.

**Exit criteria:** a user can choose a named model confidently and understand
why LocalRunner accepts, warns about, or rejects its configuration.

### 4. Local AI capabilities

**Goal:** support the most useful non-chat local workflows while preserving the
same lifecycle, queueing, and observability guarantees.

- Add embeddings.
- Add structured JSON-output mode with schema validation where supported.
- Add tool-calling request and response support.
- Evaluate reranking and document-ingestion workflows only after embeddings are
  stable.

**Exit criteria:** each added capability has bounded request validation,
consistent errors, metrics, tests, and a documented compatibility contract.

### 5. Backend abstraction

**Goal:** make LocalRunner a single local control plane rather than a hard-wired
`llama.cpp` wrapper.

- Define a runner-backend interface for lifecycle, health, chat, streaming, and
  metrics capabilities.
- Keep `llama.cpp` as the reference backend.
- Evaluate additional local backends such as MLX or other server runtimes only
  when they can meet the same localhost and observability contract.

**Exit criteria:** a second backend can be added without changing the public API
or weakening lifecycle safety.

### 6. Developer experience

**Goal:** make normal local operations visible without requiring raw HTTP calls.

- Expand `localrunnerctl` with profile management, readable streaming output,
  JSON output, and benchmark commands.
- Consider a lightweight terminal UI before a browser dashboard.
- If a web UI is added, limit it to local status, model/profile selection, live
  logs, metrics, and a test-chat panel.

**Exit criteria:** a developer can install, configure, run, inspect, and stop a
model from one coherent workflow.

### 7. Multi-model scheduling

**Goal:** safely evolve beyond one active model without promising simultaneous
inference before the hardware and scheduling design support it.

- Maintain warm/cold profile states and explicit switching transitions.
- Add memory-aware admission checks before loading a new model.
- Queue model transitions separately from inference requests and expose their
  state clearly.

**Exit criteria:** model switching cannot strand requests, exceed a configured
memory budget, or leave orphaned processes.

## Resume language after completion

Use this only when the listed functionality is implemented:

> Built LocalRunner, a local LLM runner manager in Go that supervises `llama.cpp` inference processes and exposes GGUF models through a REST API with request queuing, health checks, structured logging, and runtime metrics.

After running benchmarks, add one real measured result—such as P50 latency, token throughput, or successful recovery after a killed runner process.
