# Capsule local playground

This playground creates a two-node kind cluster and installs Capsule, Capsule
Proxy, Dex, Headlamp, and ingress-nginx through Flux. HTTP services share ports
80 and 443 through host-based ingress routing. Capsule Proxy is the only
component exposed on its own port (`9001`).

The kind API server is configured as an OIDC relying party for Dex after Dex and
ingress become ready. It uses Kubernetes' reloadable authentication config, so
there is no API-server/Dex bootstrap cycle. Dex users are mapped from the `name`
claim to the sample Capsule owners (`alice`, `bob`, and `gatsby`). These local
usernames are registered explicitly as Capsule users, and authorization comes
from tenant ownership and the RBAC managed by Capsule. The pinned Dex version
does not emit the `groups` entries from `staticPasswords`, so the `admin` login
is deliberately not granted Kubernetes `cluster-admin` by default.

## Prerequisites

- Docker
- kind
- kubectl
- Flux CLI
- GNU `envsubst` (usually provided by `gettext`)
- OpenSSL
- curl and jq
- make

The development targets (`make dev` and `make dev-capsule`) additionally need
Go and Helm. The pinned `ko` binary is installed automatically by the root
Makefile when it is not already available.

The default hostnames need to resolve to loopback. Register them in
`/etc/hosts` with:

```console
make hosts
```

This target is idempotent and prompts for `sudo` only when a hostname is
missing. The equivalent manual entry is:

```text
127.0.0.1 dex.capsule.local headlamp.capsule.local proxy.capsule.local gangplank.capsule.local
```

On macOS, verify resolution through the system resolver with:

```console
dscacheutil -q host -a name headlamp.capsule.local
```

`host` and `dig` query DNS servers directly, so they can report `NXDOMAIN` for
entries that work correctly through `/etc/hosts`.

Inside kind, CoreDNS maps the configured Dex hostname to the ingress controller.
The worker address is also registered as a host alias on the kube-apiserver
static Pod, allowing it to discover Dex's signing keys at the same issuer URL.

Then start the environment from this directory:

```console
make up
```

To start the same environment with Capsule built from the current checkout, use:

```console
make dev
```

The default endpoints are:

- Headlamp: <https://headlamp.capsule.local>
- Dex: <https://dex.capsule.local>
- Capsule Proxy: <https://proxy.capsule.local:9001>

Headlamp uses Dex for login. These local-only accounts use the username as the
password; enter the email address in Dex's login form:

| Email | Username | Password | Access |
| --- | --- | --- | --- |
| `alice@projectcapsule.dev` | `alice` | `alice` | Owns `solar`; also sees the `green` namespaces shared by the sample proxy rule |
| `bob@projectcapsule.dev` | `bob` | `bob` | Owns `green` |
| `gatsby@projectcapsule.dev` | `gatsby` | `gatsby` | Owns `wind`; also sees the `solar` namespaces shared by the sample proxy rule |
| `renewable@projectcapsule.dev` | `renewable` | `renewable` | Authenticates as the local `renewable` user |
| `admin@example.com` | `admin` | `admin` | Authenticates successfully but has no elevated Kubernetes RBAC by default |

The setup generates one persistent local CA under `installation/.generated/`
and reuses it for Dex, Headlamp, Capsule, Capsule Proxy, and kube-apiserver OIDC trust.
Changing a hostname reissues only the affected service certificate; it does not
rotate the CA. A workstation browser will report the certificate as untrusted
unless that CA is imported locally.

The reloadable API-server configuration is generated at
`installation/.generated/authentication-config.yaml`. It starts with an empty
`jwt` list while the cluster bootstraps, then `make up` replaces it with the Dex
issuer, shared CA, audience, and claim mappings after Dex is reachable.

## Configuration

Copy `.env.example` to `.env` and change the hostnames or public URLs as needed.
The Makefile supplies the shown defaults even when `.env` does not exist.
`CLUSTER_NAME` and `PROXY_PORT` can also be overridden when another local kind
cluster already uses the defaults.

To inspect the exact resources after Kustomize and environment substitution:

```console
make render
```

Substitution is restricted to the declared host and URL variables. This keeps
other dollar-prefixed content, including Dex password hashes, unchanged.

Useful lifecycle commands:

```console
make status          # inspect the cluster and Flux releases
make apply           # reapply playground configuration after editing it
make dev-capsule     # rebuild and redeploy only Capsule from the current checkout
make capsule-stable  # return Capsule to the pinned Flux-managed release
make down            # delete the kind cluster
```

## Developing Capsule in the playground

`make dev` first performs the normal playground setup, including the platform
and user examples, and then builds the Capsule controller with `ko`. The image
is loaded directly into the kind nodes and the release is upgraded from the
local `../charts/capsule` chart. Local chart templates and CRDs are therefore
deployed together with the controller code.

While a development build is installed, reconciliation of the `capsule`
HelmRelease is suspended so Flux cannot replace it with the pinned chart. The
other playground releases, including Capsule Proxy, remain managed by Flux.
The local Helm upgrade reuses the playground values and its persistent CA.

After changing the source, rebuild and roll out Capsule without recreating the
cluster:

```console
make dev-capsule
```

From the repository root, the existing `make dev-setup-capsule` target delegates
to this playground target.

The repository root's legacy `make dev-setup` target uses a different handoff:
it runs the Capsule controller on the workstation and points admission webhooks
at `LAPTOP_HOST_IP`. Before installing that development release, it waits for
`flux-system/capsule` to become ready and deletes only that HelmRelease. Waiting
for deletion lets the Flux Helm controller finish uninstalling the pinned
release before local Helm takes ownership. All other playground HelmReleases
remain managed by Flux.

```console
LAPTOP_HOST_IP=192.168.1.10 make dev-setup
```

Each invocation uses a timestamped development tag. Set one explicitly when a
predictable image name is useful:

```console
make dev-capsule DEV_VERSION=my-branch
```

The default local image is `ko.local/capsule`. Its registry and repository can
be changed with `DEV_IMAGE_REGISTRY` and `DEV_IMAGE_REPOSITORY`.

To leave development mode and restore the release declared in
`installation/capsule/release.flux.yaml`, run:

```console
make capsule-stable
```

If root `make dev-setup` deleted the Capsule HelmRelease, `capsule-stable`
recreates it from the playground installation manifests before asking Flux to
reconcile it.
