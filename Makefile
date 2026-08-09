NAME = codex-sync

VERSION ?= dev
COMMIT = $(shell git rev-parse HEAD | cut -c -8)

LDFLAGS = -ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT)"

PLATFORMS := darwin-amd64 darwin-arm64

dev:
	@echo "building bin/${NAME}"
	@go build ${LDFLAGS} -o bin/${NAME} ./cmd/${NAME}

prod: $(PLATFORMS)

$(PLATFORMS):
	@echo "building ${NAME}-$@"
	@GOOS=$(word 1,$(subst -, ,$@)) GOARCH=$(word 2,$(subst -, ,$@)) \
		go build ${LDFLAGS} -o bin/${NAME}-$@ ./cmd/${NAME}

clean:
	@rm -fr bin

.DEFAULT_GOAL := dev
.PHONY: dev prod $(PLATFORMS) clean
