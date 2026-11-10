# syntax=docker/dockerfile:1.16

# Keep builders and runtime immutable; dependency manifests are checked in.
ARG GO_IMAGE=docker.io/library/golang:1.26.10-trixie@sha256:9ce8fcba4b6fa1ae0fc1c00d255c073cfb9f9783237bb6315512fed4051d83ce
ARG RUNTIME_IMAGE=gcr.io/distroless/base-nossl-debian13:nonroot@sha256:e6b0f82595aa1edde72c8054de7a9dca30f0f8f54820c37a167c0ddfc21c5c67

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS walg-builder
ARG TARGETOS
ARG TARGETARCH
ARG WALG_REVISION=f81943e64bdf97aa66f6c52fec55114703f97af7
ARG WALG_VERSION=v3.0.8
ARG DEBIAN_SNAPSHOT=20261105T000000Z
ENV GOTOOLCHAIN=local
# Build natively, using a target C compiler for CGO when cross compiling.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    rm /etc/apt/sources.list.d/debian.sources \
    && printf 'deb [check-valid-until=no signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://snapshot.debian.org/archive/debian/%s/ trixie main\n' "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list \
    && printf 'deb [check-valid-until=no signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://snapshot.debian.org/archive/debian/%s/ trixie-updates main\n' "${DEBIAN_SNAPSHOT}" >> /etc/apt/sources.list \
    && printf 'deb [check-valid-until=no signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://snapshot.debian.org/archive/debian-security/%s/ trixie-security main\n' "${DEBIAN_SNAPSHOT}" >> /etc/apt/sources.list \
    && dpkg --add-architecture ${TARGETARCH} && apt-get update \
    && case "${TARGETARCH}" in amd64) triplet=x86_64-linux-gnu ;; arm64) triplet=aarch64-linux-gnu ;; *) exit 1 ;; esac \
    && apt-get -o Acquire::Retries=5 install -y --no-install-recommends gcc-14-$(printf %s "$triplet" | tr _ -) libc6-dev:${TARGETARCH} libbrotli-dev:${TARGETARCH} liblzo2-dev:${TARGETARCH} libsodium-dev:${TARGETARCH} \
    && printf '%s' "$triplet" > /target-triplet \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/wal-g
RUN git init && git remote add origin https://github.com/wal-g/wal-g.git \
    && git fetch --depth=1 origin ${WALG_REVISION} && git checkout --detach FETCH_HEAD
COPY build/wal-g/go.mod build/wal-g/go.sum ./
RUN go mod download && go mod verify && go mod vendor
# Link maintained Debian static libraries instead of downloading untracked C sources.
RUN sed -i 's/#cgo LDFLAGS: -lbrotli.*/#cgo LDFLAGS: -Wl,-Bstatic -lbrotlienc -lbrotlidec -lbrotlicommon -Wl,-Bdynamic -lm/' vendor/github.com/google/brotli/go/cbrotli/cgo.go \
    && sed -i 's/#cgo LDFLAGS:.*/#cgo LDFLAGS: -Wl,-Bstatic -llzo2 -Wl,-Bdynamic/' vendor/github.com/cyberdelia/lzo/lzo.go \
    && mkdir -p tmp/libsodium/lib tmp/libsodium/include /out \
    && cp -a /usr/include/sodium.h /usr/include/sodium tmp/libsodium/include/ \
    && cp /usr/lib/$(cat /target-triplet)/libsodium.a tmp/libsodium/lib/ \
    && dpkg-query -s libbrotli1:${TARGETARCH} libsodium23:${TARGETARCH} liblzo2-2:${TARGETARCH} > /out/walg-static-libraries.status \
    && printf 'postgres:x:26:26:PostgreSQL:/home/postgres:/sbin/nologin\n' > /out/passwd \
    && printf 'postgres:x:26:\n' > /out/group \
    && mkdir -p /out/licenses \
    && cp /usr/share/doc/libbrotli1/copyright /out/licenses/brotli \
    && cp /usr/share/doc/libsodium23/copyright /out/licenses/libsodium \
    && cp /usr/share/doc/liblzo2-2/copyright /out/licenses/lzo \
    && mkdir -p /out/home/postgres && chown 26:26 /out/home/postgres
# Use a standard version symbol, also read by binary inventory scanners.
# Keep ELF symbols while removing DWARF; -trimpath omits ldflags from build info.
RUN sed -i 's/\bwalgVersion\b/version/g' cmd/pg/pg.go \
    && CC=$(cat /target-triplet)-gcc-14 CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOEXPERIMENT=jsonv2 go build -mod=vendor \
    -tags brotli,libsodium,lzo -trimpath \
    -ldflags "-w -X github.com/wal-g/wal-g/cmd/pg.gitRevision=${WALG_REVISION} -X github.com/wal-g/wal-g/cmd/pg.version=${WALG_VERSION}" \
    -o /out/wal-g ./main/pg

# Retain the shell utilities used by the backup migration Job.
# Extract target utilities without replacing the builder's native shell.
RUN <<'SH'
set -eu
sed -n 's/^deb /deb-src /p' /etc/apt/sources.list > /tmp/debian-sources
cat /tmp/debian-sources >> /etc/apt/sources.list
apt-get update
apt-get install -y --no-install-recommends dpkg-dev patch xz-utils
mkdir -p /out/bin /tmp/runtime-packages
cd /tmp/runtime-packages
apt-get download "dash:$TARGETARCH" "coreutils:$TARGETARCH"
for package in ./*.deb; do
    dpkg-deb -x "$package" root
    printf '\n' >> /out/walg-static-libraries.status
    dpkg-deb -f "$package" >> /out/walg-static-libraries.status
    printf 'Status: install ok installed\n' >> /out/walg-static-libraries.status
done
cp root/usr/bin/dash root/usr/bin/sleep /out/bin/
ln -s dash /out/bin/sh
cp root/usr/share/doc/dash/copyright /out/licenses/dash
cp root/usr/share/doc/coreutils/copyright /out/licenses/coreutils

# The migration uses stream substitution, so sed needs no ACL or SELinux support.
apt-get source --only-source sed=4.9-2+deb13u1
cd sed-4.9
CC=$(cat /target-triplet)-gcc-14 ./configure --host="$(cat /target-triplet)" --disable-acl --without-selinux --disable-nls
make -j2 sed/sed
cp sed/sed /out/bin/sed
printf '\n' >> /out/walg-static-libraries.status
apt-cache show "sed:$TARGETARCH=4.9-2+deb13u1" >> /out/walg-static-libraries.status
printf 'Status: install ok installed\n' >> /out/walg-static-libraries.status
cp debian/copyright /out/licenses/sed
rm -rf /var/lib/apt/lists/*
SH

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS controller-builder
ARG TARGETOS
ARG TARGETARCH
ARG GIT_TAG=v0.3.0
ARG GIT_COMMIT=unknown
ENV GOTOOLCHAIN=local
WORKDIR /src/plugin
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=readonly -trimpath \
    -ldflags "-s -w -X github.com/wal-g/cnpg-plugin-wal-g/pkg/version.version=${GIT_TAG} -X github.com/wal-g/cnpg-plugin-wal-g/pkg/version.commitHash=${GIT_COMMIT}" \
    -o /out/cnpg-plugin-wal-g ./cmd/main.go

FROM ${RUNTIME_IMAGE} AS runtime
LABEL org.opencontainers.image.source=https://github.com/wal-g/cnpg-plugin-wal-g
LABEL org.opencontainers.image.description="CloudNativePG WAL-G Backup Plugin"
LABEL org.opencontainers.image.licenses="Apache-2.0"
ENV PATH=/usr/local/bin:/bin HOME=/home/postgres
COPY --from=walg-builder /out/passwd /etc/passwd
COPY --from=walg-builder /out/group /etc/group
COPY --from=walg-builder /out/bin/ /bin/
COPY --from=walg-builder /out/licenses /usr/share/licenses/wal-g-native
COPY --from=walg-builder --chown=26:26 /out/home/postgres /home/postgres
# Retain package metadata for C libraries linked into WAL-G so scanning sees them.
COPY --from=walg-builder /out/walg-static-libraries.status /var/lib/dpkg/status.d/walg-static-libraries
COPY --from=controller-builder --chmod=0755 /out/cnpg-plugin-wal-g /usr/local/bin/cnpg-plugin-wal-g
COPY --from=walg-builder --chmod=0755 /out/wal-g /usr/local/bin/wal-g
WORKDIR /home/postgres
USER 26:26
ENTRYPOINT ["/usr/local/bin/cnpg-plugin-wal-g"]
