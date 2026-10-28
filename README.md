# LocalRunner

LocalRunner is a localhost control plane for one `llama.cpp` model runner. It
starts and supervises `llama-server`, serializes chat requests, and exposes
health, metrics, logs, profiles, and an OpenAI-shaped chat API. `llama.cpp`
continues to perform inference.

It is for a developer running a local GGUF model—not a public service, model
registry, or inference engine.

## Quick start

Prerequisites: Go, a compatible `llama-server` executable on `PATH`, and a
local `.gguf` model.

```sh
go run ./cmd/localrunner
go run ./cmd/localrunnerctl register /absolute/path/to/model.gguf
go run ./cmd/localrunnerctl start
go run ./cmd/localrunnerctl chat "Explain LocalRunner in one sentence."
go run ./cmd/localrunnerctl stop
```

The API listens on `127.0.0.1:8080`; the managed runner listens on
`127.0.0.1:8081`. Non-loopback binds are rejected.

## Documentation

- [Getting started](docs/GETTING_STARTED.md) — profiles, the CLI, and a first request
- [API reference](docs/API.md) — endpoints, request shapes, streaming, and errors
- [Operations and configuration](docs/OPERATIONS.md) — environment variables, limits, metrics, recovery, and verification
- [Design log](DESIGN_LOG.md) — material architecture and product decisions
- [Roadmap](ROADMAP.md) — planned scope and delivery phases

## Development

```sh
make verify
make build
```

LocalRunner does not download models, accept arbitrary runner arguments, retain
prompts or completions, or support public/multi-user deployment.
