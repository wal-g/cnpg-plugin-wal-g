# CloudNativePG WAL-G Backup Plugin

*Status:* EXPERIMENTAL

This plugin adds backup and restore functionality to [CloudNativePG](https://cloudnative-pg.io/) by leveraging [WAL-G](https://github.com/wal-g/wal-g). It communicates with CloudNativePG through the `cnpg-i` interface, enabling seamless integration for managing PostgreSQL backups and restores in Kubernetes environments.

## Features

- Backup:
- [x] Full and incremental backups creation
- [x] Continuous WAL archivation/restoration
- [x] Encryption with symmetric key via `libsodium`
- [ ] Encryption with asymmetric key via `GPG` (planned)

- Restore:
- [x] Restore to any `wal-g`-created backup
- [x] Point-in-time Recovery (PITR) supporting timestamp/transaction XID/LSN

- Storage:
- [x] S3-compatible storage support (AWS S3, MinIO)
- [ ] Azure (currently not planned, need community support)
- [ ] GCS (currently not planned, need community support)

- Lifecycle:
- [x] Retention and auto-removal of outdated backups and WALs
- [ ] Marking Backups as persistent (protect from removing from storage, even if `Backup` or `BackupConfig` custom resource deleted) (planned)
- [ ] Monitoring (planned)
- [ ] `BackupConfig` Status displaying for last recoverability point, last successful backup, storage consumption (planned)

## Dependencies

- [Kubernetes](https://kubernetes.io/releases/) version 1.11 or higher
- [CloudNative PG](https://cloudnative-pg.io/releases/) version 1.25 or higher
- [Cert-manager](https://cert-manager.io/docs/releases/) version 1.13 or higher

## Quickstart

1) Install latest stable `cloudnative-pg` release and `cert-manager` (later is needed to generate certificates for secure communication between CNPG and plugin)

    ```sh
    # Install CNPG
    kubectl apply --server-side -f \
    https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.26/releases/cnpg-1.26.0.yaml

    # Install cert-manager
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml
    ```

2) Install latest plugin release into `cnpg-system` namespace (that should be the same namespace where `cloudnative-pg` installed)
- via Helm:
    ```sh
    helm -n cnpg-system upgrade --install oci://ghcr.io/wal-g/cnpg-plugin-wal-g:0.2.0-helm-chart
    ```
- or via static manifest
    ```sh
    kubectl apply -f https://raw.githubusercontent.com/wal-g/cnpg-plugin-wal-g/v0.2.0/dist/install.yaml
    ```

3) **Adjust** sample manifests from `config/samples/new-cluster` and apply
    ```sh
    kubectl apply -f ./config/samples/new-cluster
    ```

You should encounter new CNPG `Cluster` with encrypted WAL archivation and periodic auto-backups performed by plugin and `WAL-G`.

## Development

### Prerequisites
- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) version v0.27.0+ or access to existing K8s v1.11.3+ cluster.

### Run extension with Kind cluster

Bootstrap local k8s cluster with kind

```sh
./hack/kind/boostrap_kind_with_cnpg.sh
```

Build docker image locally
```sh
make docker-build
```

Install the CRDs into the cluster:
```sh
make install
```

Deploy the Manager to the cluster with the image specified by `IMG` (using deploy-kind, which injects `imagePullPolicy: Never`):
```sh
make deploy-kind
```

### Remove && cleanup Kind cluster

```sh
./hack/kind/cleanup_kind.sh
```

### Run extension with the external cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/cnpg-plugin-wal-g:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/cnpg-plugin-wal-g:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -f config/samples/new-cluster
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -f config/samples/new-cluster
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```
