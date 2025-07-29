# WAL-G Build section
# Need to build from sources, because binary distributions do not include necessary base OS libs
# Use docker.io/golang@1.23-bookworm as of 29.07.2025
FROM docker.io/golang@sha256:a9f66e33b22953f2a5340c36bc5aceef29c1ac8f2ad7c362bf151f9571788eb6 AS walg-builder

LABEL org.opencontainers.image.source=https://github.com/wal-g/cnpg-plugin-wal-g
LABEL org.opencontainers.image.description="CloudNativePG WAL-G Backup Plugin"
LABEL org.opencontainers.image.licenses="Apache-2.0"

ARG TARGETOS=linux
ARG TARGETARCH=amd64
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
    make pg_build && make pg_install

# Runtime image section
#
# Primary runtime image (using bookworm-slim due to wal-g dynamic linked C dependencies)
FROM docker.io/debian:bookworm-slim AS runtime
RUN apt update && apt install -y ca-certificates && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN groupadd -o -g 26 postgres && useradd -ms /bin/bash -u 26 -g postgres postgres
WORKDIR /home/postgres
COPY --chmod=0755 /output/cnpg-plugin-wal-g /usr/local/bin/cnpg-plugin-wal-g
COPY --from=walg-builder /wal-g /usr/local/bin/wal-g
# Using same user as postgres user in postgresql instances to be able to manage files in PGDATA
USER 26:26

ENTRYPOINT ["/usr/local/bin/cnpg-plugin-wal-g"]
