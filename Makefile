BINARY      := clauded
PKG         := github.com/addodelgrossi/clauded
CMD         := ./cmd/clauded
VERSION_PKG := $(PKG)/internal/version

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE)

PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build run test test-integration lint vet fmt cross release clean tidy

build: ## Compila o binário para a plataforma atual
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) $(CMD)

run: build ## Compila e roda
	./dist/$(BINARY)

test: ## Roda os testes unitários
	go test -race -cover ./...

test-integration: ## Roda o teste de integração (requer claude + token)
	go test -tags=integration -v ./internal/runner -run Integration

vet: ## go vet
	go vet ./...

lint: ## golangci-lint (requer instalação)
	golangci-lint run

fmt: ## Formata o código
	gofmt -w .

tidy: ## go mod tidy
	go mod tidy

cross: ## Cross-compila os 5 alvos manualmente (sem goreleaser)
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "==> $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath \
			-ldflags "$(LDFLAGS)" \
			-o dist/$(BINARY)-$$os-$$arch$$ext $(CMD) || exit 1; \
	done
	@echo "Binários em dist/"

release: ## Release com goreleaser (requer tag e GITHUB_TOKEN)
	goreleaser release --clean

clean: ## Remove artefatos de build
	rm -rf dist/
