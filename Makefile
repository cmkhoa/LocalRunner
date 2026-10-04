.PHONY: fmt check-format test vet verify run

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

check-format:
	@test -z "$$(gofmt -l $$(find cmd internal -name '*.go' -type f))"

test:
	go test ./...

vet:
	go vet ./...

verify: check-format test vet

run:
	go run ./cmd/localrunner
