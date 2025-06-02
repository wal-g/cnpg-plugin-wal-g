# Build the manager binary
FROM docker.io/golang:1.23-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY internal/ internal/

# Build extensions binary
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o cnpg-plugin-wal-g cmd/main.go


# WAL-G Build section
# Need to build from sources, because binary distributions do not include necessary base OS libs
FROM docker.io/golang:1.23-bookworm AS walg-builder

ARG TARGETOS
ARG TARGETARCH
ARG WALG_VERSION=v3.0.5
ARG WALG_REPO=https://github.com/wal-g/wal-g

# build arguments for wal-g
ARG USE_BROTLI=1
ARG USE_LIBSODIUM=1
ARG USE_LZO=1

# Install lib dependencies
RUN apt update && apt install -y libbrotli-dev liblzo2-dev libsodium-dev curl cmake git
# Clone wal-g sources
RUN git clone --depth 1 --branch ${WALG_VERSION} ${WALG_REPO} $(go env GOPATH)/src/github.com/wal-g/wal-g

# Preparing && building necessary dependencies
RUN cd $(go env GOPATH)/src/github.com/wal-g/wal-g && \
    export GOOS=${TARGETOS:-linux} && \
    export GOARCH=${TARGETARCH} && \
    make deps

# Build wal-g for postgresql
RUN cd $(go env GOPATH)/src/github.com/wal-g/wal-g && \
    export GOOS=${TARGETOS:-linux} && \
    export GOARCH=${TARGETARCH} && \
    make pg_build && make pg_install

# Runtime image section
#
# Primary runtime image (using bookworm-slim due to wal-g dynamic linked C dependencies)
FROM docker.io/debian:bookworm-slim AS runtime
RUN apt update && apt install -y ca-certificates && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN groupadd -o -g 26 postgres && useradd -Ms /bin/bash -u 26 -g postgres postgres
WORKDIR /
COPY --from=builder /workspace/cnpg-plugin-wal-g /usr/local/bin/cnpg-plugin-wal-g
COPY --from=walg-builder /wal-g /usr/local/bin/wal-g
# Using same user as postgres user in postgresql instances to be able to manage files in PGDATA
USER 26:26

ENTRYPOINT ["/usr/local/bin/cnpg-plugin-wal-g"]
