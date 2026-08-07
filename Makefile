.PHONY: build install test vet

build:
	go build -o bin/chatgpt-settings-sync ./cmd/chatgpt-settings-sync

install:
	mkdir -p "$(HOME)/.local/bin"
	go build -o "$(HOME)/.local/bin/chatgpt-settings-sync" ./cmd/chatgpt-settings-sync

test:
	go test ./...

vet:
	go vet ./...
