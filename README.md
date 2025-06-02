# CloudNativePG WAL-G Backup Plugin

This plugin adds backup and restore functionality to [CloudNativePG](https://cloudnative-pg.io/) by leveraging [WAL-G](https://github.com/wal-g/wal-g). It communicates with CloudNativePG through the `cnpg-i` interface, enabling seamless integration for managing PostgreSQL backups and restores in Kubernetes environments.

## Getting Started

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
./hack/kind/cleanup.sh
```

### To Deploy on the external cluster
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
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/cnpg-plugin-wal-g:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/cnpg-plugin-wal-g/<tag or branch>/dist/install.yaml
```
