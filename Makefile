.PHONY: build test lint run clean

BINARY := agent-governance-core
CMD := ./cmd/agent-governance-core

build:
	go build -o bin/$(BINARY) $(CMD)

run: build
	./bin/$(BINARY)

test:
	go test ./... -v -race -count=1

test-short:
	go test ./... -short -count=1

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

tidy:
	go mod tidy

test-integration:
	go test ./test/integration/... -v -race -count=1 -tags=integration

.DEFAULT_GOAL := build
