# deckdex build orchestration. Everything runs in Docker — no host Go needed.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_IMAGE ?= golang:1.26-alpine

.PHONY: build deps tidy vet test clean

## build: cross-compile all release binaries into ./dist
build:
	DOCKER_BUILDKIT=1 docker build \
		--build-arg VERSION=$(VERSION) \
		--target export \
		--output type=local,dest=dist .
	@echo "binaries in ./dist:"
	@ls -1 dist

## deps: resolve go.mod/go.sum in a container (run after changing imports)
deps tidy:
	docker run --rm -v "$(CURDIR)":/src -w /src $(GO_IMAGE) \
		sh -c "go mod tidy"

## vet: run go vet in a container
vet:
	docker run --rm -v "$(CURDIR)":/src -w /src $(GO_IMAGE) \
		sh -c "go vet ./..."

## test: run unit tests in a container
test:
	docker run --rm -v "$(CURDIR)":/src -w /src $(GO_IMAGE) \
		sh -c "go test ./..."

## clean: remove build artifacts
clean:
	rm -rf dist
