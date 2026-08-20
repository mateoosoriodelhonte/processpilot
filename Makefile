.PHONY: all fmt lint test integration security check build run demo browser-test package

all: check build

fmt:
	cargo fmt --check
	test -z "$$(gofmt -l .)"

lint:
	cargo clippy --workspace --all-targets --locked -- -D warnings
	go vet ./...

test:
	cargo test --workspace --locked
	go test ./...

integration:
	cargo build --locked
	go test -tags=integration ./integration

security:
	cargo test --locked --test privacy --test collector_binary
	go test ./internal/security ./internal/ai ./internal/protocol

check: fmt lint test integration security

build:
	cargo build --release --locked
	mkdir -p bin
	go build -trimpath -ldflags="-s -w" -o bin/processpilot ./cmd/processpilot
	cp target/release/processpilot-collector bin/processpilot-collector

run: build
	./bin/processpilot

demo:
	mkdir -p bin
	go build -trimpath -o bin/processpilot ./cmd/processpilot
	./bin/processpilot demo

browser-test:
	npm test

package:
	./scripts/package.sh
