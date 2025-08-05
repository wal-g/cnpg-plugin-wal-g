# syntax = docker/dockerfile:1.16

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
    export GOOS=${TARGETOS} && \
    export GOARCH=${TARGETARCH} && \
    make deps

# Build wal-g for postgresql
RUN cd $(go env GOPATH)/src/github.com/wal-g/wal-g && \
    export GOOS=${TARGETOS} && \
    export GOARCH=${TARGETARCH} && \
    make pg_build && make pg_install && /wal-g --version

# Controller Build section
#
FROM docker.io/golang:1.23-bookworm AS controller-builder
ARG TARGETOS
ARG TARGETARCH
ARG GIT_TAG=v0.0.0-dev
ARG GIT_COMMIT=f07cc65b11df5506d85f8b143c64884c1dcf24a4
COPY go.mod go.sum /cnpg-plugin-wal-g/
WORKDIR /cnpg-plugin-wal-g
ENV GOOS=$TARGETOS GOARCH=$TARGETARCH
RUN go mod download
COPY . /cnpg-plugin-wal-g/
RUN make build && ./output/cnpg-plugin-wal-g version

# Runtime image section
#
# Primary runtime image (using bookworm-slim due to wal-g dynamic linked C dependencies)
FROM docker.io/debian:bookworm-slim AS runtime
LABEL org.opencontainers.image.source=https://github.com/wal-g/cnpg-plugin-wal-g
LABEL org.opencontainers.image.description="CloudNativePG WAL-G Backup Plugin"
LABEL org.opencontainers.image.licenses="Apache-2.0"

RUN apt update && apt install -y ca-certificates && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN groupadd -o -g 26 postgres && useradd -ms /bin/bash -u 26 -g postgres postgres
WORKDIR /home/postgres
COPY --from=controller-builder --chmod=0755 /cnpg-plugin-wal-g/output/cnpg-plugin-wal-g /usr/local/bin/cnpg-plugin-wal-g
COPY --from=walg-builder /wal-g /usr/local/bin/wal-g
# Using same user as postgres user in postgresql instances to be able to manage files in PGDATA
USER 26:26

ENTRYPOINT ["/usr/local/bin/cnpg-plugin-wal-g"]
