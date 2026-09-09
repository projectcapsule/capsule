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

## Resource permit examples

The platform Kustomization includes two additional `GlobalResourcePermitTemplate`
examples. Their sample requests live in `user/solar/resourcepermits/` and are
submitted as `alice` by `make apply-user`. Use a development build with the CRDs
from this checkout (`make dev-capsule`) when trying these APIs.

| Template | Result after approval | Lifetime |
| --- | --- | --- |
| `gateway-api` | RoleBindings grant all current solar owners CRUD access to namespaced Gateway API resources in every solar namespace. | Permanent: the distributor is retained after the provisioning permit expires, and continues updating owners and namespaces. |
| `grafana` | Creates `solar-grafana-main` and distributes a Flux HelmRepository, HelmRelease, and namespace-scoped deployment identity into it. | No automatic expiry by default; explicitly expiring the permit removes the instance and its namespace. |

The sample permits are named `gateway-api-access` and `managed-grafana`.
Neither has an automatic expiry; an approver can expire them explicitly.

Both templates derive tenant ownership from the request namespace's Capsule
label. Neither accepts a tenant name or arbitrary RBAC subjects as parameters.
The default ServiceAccount must exist in the namespace submitting the request;
it supplies the trusted namespace name when loading template context. Grafana
explicitly executes as the playground's `capsule-system/capsule` controller
ServiceAccount so namespace admission adds the Tenant owner reference. The
generated GlobalTenantResources use `capsule-system/permit-example-reconciler` with the
permissions declared in `platform/globalresourcepermittemplates/rbac.yaml`.

To install just these templates and submit the requests to an existing
playground, run from this directory:

```console
kubectl --context kind-capsule apply -k platform/globalresourcepermittemplates
kubectl --context kind-capsule get globalresourcepermittemplates
kubectl --context kind-capsule --as alice --as-group projectcapsule.dev apply -f user/solar/resourcepermits/gateway-api-access.yaml
kubectl --context kind-capsule --as alice --as-group projectcapsule.dev apply -f user/solar/resourcepermits/managed-grafana.yaml
kubectl --context kind-capsule -n solar-system get resourcepermits
```

Wait until `solar-system` appears in each template's `status.namespaces` before
submitting requests. The requests require approval by `kubernetes-admin` or
`admin`. Once their phase is `Requested`, inspect `status.request.resources`
and approve them through Headlamp or the status subresource:

```console
kubectl --context kind-capsule -n solar-system get resourcepermit gateway-api-access managed-grafana -o yaml
kubectl --context kind-capsule --as admin -n solar-system patch resourcepermit gateway-api-access --subresource=status --type=merge -p '{"status":{"phase":"Approved"}}'
kubectl --context kind-capsule --as admin -n solar-system patch resourcepermit managed-grafana --subresource=status --type=merge -p '{"status":{"phase":"Approved"}}'
```

The Gateway API example installs RBAC only. Install the
[Gateway API CRDs and a compatible controller](https://gateway-api.sigs.k8s.io/guides/getting-started/introduction/)
to create working routes. GatewayClasses and CRD definitions remain under
platform control. Inspect the resulting bindings with:

```console
kubectl --context kind-capsule get globaltenantresource solar-gateway-api-access
kubectl --context kind-capsule -n solar-test get rolebinding gateway-api-editors -o yaml
kubectl --context kind-capsule --as alice -n solar-test auth can-i create httproutes.gateway.networking.k8s.io
```

The Grafana example uses the
[Grafana community chart](https://grafana-community.github.io/helm-charts/) and
[Flux HelmRelease API](https://fluxcd.io/flux/components/helm/helmreleases/).
Its distributor places the HelmRelease in the new namespace after creation;
this avoids dry-running a namespaced resource before its namespace exists.
Flux runs Helm as `grafana-reconciler`, which has deployment permissions only
in that namespace. A permit becoming `Active` means its namespace and distributor
have been applied; wait separately for the HelmRelease to become ready:

```console
kubectl --context kind-capsule get globaltenantresource solar-grafana-main
kubectl --context kind-capsule -n solar-grafana-main get helmrepository,helmrelease
kubectl --context kind-capsule -n solar-grafana-main wait --for=condition=Ready helmrelease/grafana --timeout=10m
kubectl --context kind-capsule --as alice -n solar-grafana-main get secret grafana -o jsonpath='{.data.admin-password}' | base64 --decode
kubectl --context kind-capsule --as alice -n solar-grafana-main port-forward service/grafana 3000:80
```

Open <http://localhost:3000> and log in as `admin` with the generated password.
Storage is ephemeral for the local playground: dashboards and other data are
lost when the Grafana Pod is replaced. To request another instance, use a new
ResourcePermit name and change `params.instance`; the generated namespace is
`<tenant>-grafana-<instance>` and must fit in 63 characters.

To remove Grafana, expire its permit as an approver. To revoke permanent Gateway
API access, expire its provisioning permit and wait for it to be removed, then
delete the retained distributor; its RoleBindings are pruned on deletion:

```console
kubectl --context kind-capsule --as admin -n solar-system patch resourcepermit managed-grafana --subresource=status --type=merge -p '{"status":{"phase":"Expired"}}'
kubectl --context kind-capsule --as admin -n solar-system patch resourcepermit gateway-api-access --subresource=status --type=merge -p '{"status":{"phase":"Expired"}}'
kubectl --context kind-capsule -n solar-system wait --for=delete resourcepermit/gateway-api-access --timeout=2m
kubectl --context kind-capsule delete globaltenantresource solar-gateway-api-access
```

These templates do not retain expired requests. Grafana cleanup deletes the
dedicated namespace and everything in it.
