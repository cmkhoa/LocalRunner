# LocalRunner Design Log

This log records the reasoning behind material product and technical choices.
Each implementation turn adds a short entry describing the choice, what changed,
and the tradeoff accepted.

## 2026-10-27 — Establish the decision log

**Choice:** Keep a human-readable, repository-local design log rather than
relying on commit messages or chat history alone.

**Why:** LocalRunner is expanding beyond its original weekend MVP. Future work
needs an auditable record of scope boundaries, compatibility decisions, and
operational tradeoffs.

**Consequence:** Each substantive implementation turn appends a dated entry and
the user receives a concise progress explanation in chat.

## 2026-10-04 — Local control plane, not a new inference engine

**Choice:** Delegate inference to `llama.cpp`; make LocalRunner responsible for
process lifecycle, queueing, API behavior, health, logs, and metrics.

**Why:** This creates a useful, testable local service without recreating
quantized inference kernels, tokenizers, GPU backends, and model architectures.

**Consequence:** LocalRunner initially supports one active GGUF model and a
single `llama.cpp` subprocess.

## 2026-10-04 — Localhost-only, one-worker request execution

**Choice:** Bind the API and managed runner to loopback addresses and serialize
requests through one bounded FIFO worker.

**Why:** The first target is a single developer's local machine, where runner
overload and orphaned processes are more immediate risks than multi-user scale.

**Consequence:** Public deployment, tenancy, and unbounded concurrency remain
out of scope; queue limits must be explicit and observable.

## 2026-10-04 — Explicit paths and named profiles

**Choice:** Accept one-off GGUF paths only after validation, and add named
profiles for repeatable model/context/GPU settings.

**Why:** Avoid arbitrary shell input while making everyday local use practical.

**Consequence:** Raw runner arguments are intentionally not part of the public
API; profiles expose only reviewed, structured settings.

## 2026-10-04 — Streaming preserves queue ownership

**Choice:** A streamed completion keeps the single queue worker until its SSE
response ends.

**Why:** Releasing the worker at stream start would allow concurrent inference
requests to overload the active model.

**Consequence:** Streaming is supported, but a long stream deliberately delays
later queued work.

## 2026-10-04 — Opt-in automatic restart with a bounded budget

**Choice:** Automatic runner restart is disabled by default and limited by a
configured delay and maximum attempts.

**Why:** Recovery is useful, but silent crash loops are dangerous and obscure
root causes.

**Consequence:** Unexpected exits become visible as `unhealthy` before a retry;
explicit stops never schedule a restart.

## 2026-10-27 — OpenAI-compatible subset, not an unqualified claim

**Choice:** Expose only tested OpenAI-shaped endpoints and payloads, beginning
with `/v1/models` and chat completions.

**Why:** Existing local tools benefit from familiar shapes, but compatibility
must remain honest and documented.

**Consequence:** Local model IDs are profile names or active-model names, and
local filesystem paths are never exposed by `/v1/models`.

## 2026-10-27 — Global API runtime limits

**Choice:** Make request-size, input-size, generation, queue-capacity, and
timeout limits configurable only at service startup.

**Why:** Queue capacity is allocated when the handler starts. Changing limits
per request or profile would create unclear behavior for work already queued.

**Consequence:** Operators can tune a local machine without a rebuild, but
must restart LocalRunner for a change to take effect. Profile-specific limits
remain a later design task with explicit reconfiguration semantics.

## 2026-10-27 — Bounded failure diagnostics in runner status

**Choice:** Retain a short, sanitized cause for failed readiness checks and
unexpected child exits as `last_error` on runner status.

**Why:** A lifecycle state such as `unhealthy` identifies the symptom but not
the cause. Operators need enough context to investigate without reading a
full log stream.

**Consequence:** Diagnostics are stripped of control characters and capped at
256 bytes; they intentionally never include request prompts or responses.

## 2026-10-27 — Cancellation is a queue-level guarantee

**Choice:** Treat a canceled request that is waiting in the FIFO queue as work
that must never be sent upstream.

**Why:** Letting a disconnected client consume inference time would make a
single local runner unresponsive under retries or abandoned streams.

**Consequence:** The queue checks the request context immediately before it
invokes work, and a regression test covers cancellation after enqueueing.

## 2026-10-27 — README as an entry point, detailed docs by task

**Choice:** Keep the root README focused on the product, a fast first run, and
links; organize setup, API, and operations details in dedicated guides.

**Why:** A single long README makes both onboarding and reference lookup slow
as the service gains operational features.

**Consequence:** The root page stays concise while the detailed guides can grow
independently without obscuring the normal developer workflow.

## 2026-10-27 — Proxy integration tests use a controlled runner surface

**Choice:** Test the API against a local mock runner through the same HTTP
proxy path used in production, including streaming cancellation.

**Why:** Unit tests cannot prove that client cancellation travels through the
API, queue worker, HTTP client, and upstream response stream.

**Consequence:** The API depends on a narrow internal runner interface, while
the production `runner.Manager` remains its implementation.
