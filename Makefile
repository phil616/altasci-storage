SHELL := /bin/sh

GO ?= go
NPM ?= npm
VERSION ?= 0.1.0
GO_BUILD_FLAGS ?= -v
DIST_DIR := $(CURDIR)/dist
SERVER_BIN := $(DIST_DIR)/altasci-server

.PHONY: help doctor build build-backend build-frontend init run test

help:
	@echo "Available targets:"
	@echo "  make doctor          Show required tool versions"
	@echo "  make build           Build backend and frontend"
	@echo "  make build-backend   Create dist/altasci-server"
	@echo "  make build-frontend  Create frontend/dist/"
	@echo "  make init INIT_ARGS='--admin-email ... --public-web-url ... --public-api-url ...'"
	@echo "  make run             Start the initialized backend"
	@echo "  make test            Run backend tests/vet and frontend production build"

doctor:
	@command -v $(GO) >/dev/null 2>&1 || { echo "ERROR: Go is not installed or not on PATH"; exit 1; }
	@command -v cc >/dev/null 2>&1 || { echo "ERROR: a C compiler is required by go-sqlite3"; exit 1; }
	@command -v node >/dev/null 2>&1 || { echo "ERROR: Node.js is not installed or not on PATH"; exit 1; }
	@command -v $(NPM) >/dev/null 2>&1 || { echo "ERROR: npm is not installed or not on PATH"; exit 1; }
	@echo "Host Go:    $$($(GO) version)"
	@echo "Backend Go: $$(cd backend && $(GO) env GOVERSION)"
	@echo "C:    $$(cc --version | head -n 1)"
	@echo "Node: $$(node --version)"
	@echo "npm:  $$($(NPM) --version)"

build: build-backend build-frontend

build-backend:
	@echo "[build] compiling Go backend (the first build may download the Go toolchain and modules)"
	@mkdir -p "$(DIST_DIR)"
	cd backend && CGO_ENABLED=1 $(GO) build $(GO_BUILD_FLAGS) -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$(SERVER_BIN)" ./cmd/altasci-server
	@echo "[build] backend binary: $(SERVER_BIN)"

build-frontend:
	@echo "[build] installing locked frontend dependencies"
	cd frontend && $(NPM) ci --loglevel=info --no-audit --no-fund
	@echo "[build] compiling frontend"
	cd frontend && $(NPM) run build
	@echo "[build] frontend files: $(CURDIR)/frontend/dist"

init: build-backend
	@if [ -z "$(INIT_ARGS)" ]; then echo "ERROR: set INIT_ARGS; run 'make help' for an example"; exit 1; fi
	cd backend && "$(SERVER_BIN)" init $(INIT_ARGS)

run: build-backend
	cd backend && "$(SERVER_BIN)" serve --config config.toml

test:
	cd backend && $(GO) test ./...
	cd backend && $(GO) vet ./...
	cd frontend && $(NPM) run build
	cd frontend && $(NPM) run test:e2e
