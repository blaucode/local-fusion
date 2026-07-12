# local-fusion v2 — one static binary in a distroless image (ADR-001/002).
# Build args mirror the Makefile; nothing here needs a host toolchain.

FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=2.0.0-dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build \
    -ldflags "-X local-fusion/internal/version.Version=${VERSION} -X local-fusion/internal/version.Commit=${COMMIT}" \
    -o /out/local-fusion ./cmd/local-fusion
# Pre-owned /data: distroless runs as nonroot (65532); a fresh volume copies
# this dir's ownership, so the server can write it.
RUN mkdir -p /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/local-fusion /local-fusion
# Artifact volume (ADR-005). Keys/token arrive via --env-file, never baked in.
COPY --from=build --chown=65532:65532 /data /data
VOLUME ["/data"]
EXPOSE 8484
# --insecure-no-token: inside the container the process must bind 0.0.0.0; the
# loopback-only guarantee is docker's published port (make docker-run publishes
# 127.0.0.1:8484). Set LF_AUTH_TOKEN when publishing beyond localhost.
ENTRYPOINT ["/local-fusion"]
CMD ["serve", "--addr", ":8484", "--insecure-no-token"]
