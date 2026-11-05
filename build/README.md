# Security build

The Dockerfile builds the stable plugin v0.3.0 with updated Go dependencies and
WAL-G v3.0.8 from commit f81943e64bdf97aa66f6c52fec55114703f97af7. WAL-G dependency
manifests are locked in `build/wal-g/`; the build does not run `go get`.

Go 1.26.10 and the distroless Debian 13 runtime are pinned by image digest.
Native libraries are installed from a dated Debian snapshot with signed metadata.
To update security fixes later, update the image digests, snapshot and dependency
manifests together, then repeat scanning and functional checks.

WAL-G retains brotli, libsodium and lzo support. It links Debian static libraries
instead of the old bundled Brotli source. Their Debian package metadata is kept
in the final image so vulnerability scanners can identify the embedded libraries.
Only libc and libm remain dynamic dependencies; the runtime includes glibc,
certificate authorities and timezone data. UID/GID 26 and HOME are preserved.
The direct WAL-G calls do not require shell utilities. OMDB backup migration Jobs
invoke `/bin/sh`, `sed` and `sleep` in this image. Debian dash and coreutils sleep
are retained. GNU sed is built from Debian's patched source without ACL/SELinux
support, which the migration's stream substitutions do not use. Their metadata
is retained for scanning; curl, procps and BusyBox are not included.

Build locally without publishing:

```sh
docker buildx build --platform linux/amd64 --load \
  --build-arg GIT_COMMIT="$(git rev-parse HEAD)" \
  -t cnpg-plugin-wal-g:omdb-791 .
docker run --rm --platform linux/amd64 cnpg-plugin-wal-g:omdb-791 version
docker run --rm --platform linux/amd64 --entrypoint /usr/local/bin/wal-g \
  cnpg-plugin-wal-g:omdb-791 --version
```

The builders run natively and cross-compile CGO when needed. amd64 and arm64 are
supported by the Dockerfile; test each target used in production.

The `build-artifacts` target exports WAL-G, its package metadata and the upstream
libsodium test executable. Run that test with its source `testdata` directory
on the target architecture to verify encryption and decryption. The test binary
is not copied to the runtime image.

Before rollout, run plugin unit/envtest suites and scan the exported image archive
or local Docker image. After publishing, repeat the task's remote Trivy commands
against the registry digest. Backup/restore, PITR, retention and an update from
the previous version require a separate isolated integration environment.

WAL-G identifies this custom build as `v3.0.8+omdb.791`. Its existing version
variable is renamed to the conventional `cmd/pg.version` during compilation.
The linker injects the actual release/build version there. ELF symbols are kept
(`-w` removes DWARF, while `-s` is intentionally omitted) so binary scanners can
read this version even when `-trimpath` omits linker flags from Go build info.
Git revision and modified-source information remain embedded. No vulnerability
ignore rules or version-string replacements in the finished binary are used.
