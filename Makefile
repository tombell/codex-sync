.PHONY: build install test vet

build:
	go build -o bin/codex-sync ./cmd/codex-sync

install:
	mkdir -p "$(HOME)/.local/bin"
	go build -o "$(HOME)/.local/bin/codex-sync" ./cmd/codex-sync

test:
	go test ./...

vet:
	go vet ./...
