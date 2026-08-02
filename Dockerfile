# Build a static capsule binary and ship it in a small image.
#
#   docker build -t capsule .
#   docker run --rm capsule version
#
# capsule drives a container runtime, so running capsule *itself* in a container
# needs the host's Docker socket. That grants the container root-equivalent
# control of the host — only do it on a machine you trust, and prefer the native
# binary from the releases page for everyday use:
#
#   docker run --rm -it \
#     -v /var/run/docker.sock:/var/run/docker.sock \
#     -v "$PWD:$PWD" -w "$PWD" \
#     ghcr.io/martin-k-m/capsule up
FROM golang:1.24-alpine AS build
WORKDIR /src

# Nothing to download — capsule has no third-party dependencies — but copying
# go.mod first still keeps this layer stable across source edits.
COPY go.mod ./
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/martin-k-m/capsule/internal/cli.Version=${VERSION}" \
      -o /out/capsule ./cmd/capsule

FROM alpine:3.20
LABEL org.opencontainers.image.source="https://github.com/martin-k-m/capsule" \
      org.opencontainers.image.description="Lightweight, isolated development environments that disappear when you're done" \
      org.opencontainers.image.licenses="Apache-2.0"

# capsule shells out to a container CLI rather than talking to a daemon socket
# itself, so the image needs one present.
RUN apk add --no-cache docker-cli

COPY --from=build /out/capsule /usr/local/bin/capsule
WORKDIR /workspace
ENTRYPOINT ["capsule"]
CMD ["--help"]
