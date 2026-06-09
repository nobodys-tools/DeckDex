#!/bin/sh
# Cross-compile deckdex for every release target into $OUT (default /out).
# Invoked inside the Docker builder stage; CGO is disabled for static binaries.
set -eu

VERSION="${VERSION:-dev}"
OUT="${OUT:-/out}"
mkdir -p "$OUT"

TARGETS="linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64"

for target in $TARGETS; do
	GOOS="${target%/*}"
	GOARCH="${target#*/}"
	ext=""
	[ "$GOOS" = "windows" ] && ext=".exe"
	name="deckdex-${GOOS}-${GOARCH}${ext}"
	echo ">> building ${name}"
	CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
		go build -trimpath \
		-ldflags "-s -w -X main.version=${VERSION}" \
		-o "${OUT}/${name}" ./cmd/deckdex
done

echo ">> done; artifacts in ${OUT}"
