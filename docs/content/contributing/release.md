# Release Process

The Capsule release process is constrained to _GitHub Releases_, following the git tag semantic versioning.

## Semantic versioning convention

Capsule is taking advantage of the [Semantic Versioning](https://semver.org/), although with some rules about the patch, the minor and the major bump versions.

- `patch` (e.g.: 0.1.0 to 0.1.1):
  a patch bumping occurs when some bugs are fixed, and no Kubernetes CRDs API changes are introduced.
  The patch can contain also new features not yet promoted to a specific Kubernetes CRDs API type.
  A patch may be used also to address CVE patches.
- `minor` (e.g.: 0.1.0 to 0.2.0):
  a minor bumping occurs when a new CRDs API object is introduced, or rather, when some CRDs schemes are updated.
  The minor bump is used to inform the Capsule adopters to manually update the Capsule CRDs, since Helm, the suggested tool for the release lifecycle management, is not able to automatically update the objects.
  Upon every minor release, on the GitHub Release page, a list of API updates is described, and a link to the [upgrade guide](https://capsule.clastix.io/docs/guides/upgrading) is provided.
- `major` (e.g.: 0.1.0 to 1.0.0):
  a major bump occurs when a breaking change, such as backward incompatible changes is introduced.

## Container hosting

All the Capsule container images are publicly hosted on [CLASTIX](https://clastix.io) [Docker Hub repository](https://hub.docker.com/r/clastix/capsule).

The Capsule container image is built upon a git tag (issued thanks to the _GitHub Release_ feature) starting with the prefix `v` (e.g.: `v1.0.1`).
This will trigger a _GitHub Action_ which builds a multi-arch container image, then pushes it to the container registry.

> The `latest` tag is not available to avoid moving git commit SHA reference.

## Helm Chart hosting

The suggested installation tool is [Helm](https://helm.sh), and the Capsule chart is hosted in the [GitHub repository](https://github.com/clastix/capsule/tree/master/charts/capsule).
For each Helm Chart release, a tit tag with the prefix `helm-v` will be issued to help developers to address the corresponding commit.

The built Helm Charts are then automatically pushed upon tag release to the [CLASTIX Helm repository](https://clastix.github.io/charts).
