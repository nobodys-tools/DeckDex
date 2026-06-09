# syntax=docker/dockerfile:1
#
# Reproducible cross-platform build for deckdex. The host needs only Docker.
#
#   make build      -> binaries land in ./dist
#
# The builder stage compiles the GOOS/GOARCH matrix with CGO disabled (static,
# portable binaries); the final `export` stage is a scratch image whose sole
# purpose is to hand the artifacts back to the host via BuildKit --output.

FROM golang:1.26-alpine AS builder
WORKDIR /src

# Cache module downloads independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN VERSION=${VERSION} OUT=/out sh scripts/build.sh

# Artifact-only stage: `docker build --target export --output type=local,dest=dist`
FROM scratch AS export
COPY --from=builder /out/ /
