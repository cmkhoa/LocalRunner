# Getting Started

## Prerequisites

- Go
- A compatible `llama.cpp` `llama-server` executable on `PATH` (or set
  `LOCALRUNNER_LLAMA_SERVER_PATH`)
- A local, non-empty `.gguf` model file

## Start a one-off model

In one terminal, run the service:

```sh
go run ./cmd/localrunner
```

In another, use the bundled CLI:

```sh
go run ./cmd/localrunnerctl register /absolute/path/to/model.gguf
go run ./cmd/localrunnerctl start
go run ./cmd/localrunnerctl status
go run ./cmd/localrunnerctl chat "Say hello in one sentence."
go run ./cmd/localrunnerctl metrics
go run ./cmd/localrunnerctl stop
```

Use `--stream` to print raw server-sent events:

```sh
go run ./cmd/localrunnerctl chat --stream "Count from one to three."
```

Set `LOCALRUNNER_API_URL` or pass `--address http://127.0.0.1:8183` to point
the CLI at another local API address.

## Use named profiles

For repeatable settings, copy and edit
[the example profile file](../configs/profiles.example.json), then start with:

```sh
LOCALRUNNER_PROFILES_PATH=/absolute/path/to/profiles.json go run ./cmd/localrunner
go run ./cmd/localrunnerctl profiles
go run ./cmd/localrunnerctl activate qwen3-4b
go run ./cmd/localrunnerctl start
```

Profiles include a model path, context size, and GPU-layer setting. A profile
may be activated only while the runner is stopped or unhealthy.

For endpoint-level examples, see the [API reference](API.md). For limits and
environment variables, see [Operations and configuration](OPERATIONS.md).
