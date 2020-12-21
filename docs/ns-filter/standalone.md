# Standalone Installation
The `capsule-ns-filter` can be deployed in standalone mode, e.g. running as a pod bridging any Kubernetes client to the `kube-apiserver`. Use this way to provide access to client-side command line tools like `kubectl` or even client-side dashboards.

You can use an Ingress Controller to expose the `capsule-ns-filter` endpoint or, depending on your environment, you can expose it with either a `NodePort`, or a `LoadBalancer` service. As alternatives, use `HostPort` or `HostNetwork` mode.

```
                +-----------+          +-----------+         +-----------+
 kubectl ------>|:443       |--------->|:9001      |-------->|:6443      |
                +-----------+          +-----------+         +-----------+
                ingress-controller     capsule-ns-filter     kube-apiserver
                (ssl-passthrough)
``` 

The [standalone-setup.yaml](../deploy/standalone-setup.yaml) manifest contains an example for deploying with Ingress Controller in ssl-passthrough mode.

## Arguments
Arguments to be passed to the `capsule-ns-filter` proxy:

```
--listening-port         HTTP port the proxy listens to, default: 9001
--k8s-control-plane-url  Kubernetes control plane URL, default: https://kubernetes.default.svc
--capsule-user-group     The Capsule User Group, default: capsule.clastix.io
--zap-devel              Enable debug
--zap-log-level          Set log verbosity, from 1 to 10
--enable-ssl             Enable the bind on HTTPS for secure communication, default: false
--ssl-cert-path          Path to the TLS certificate, default: /opt/capsule-ns-filter/tls.crt
--ssl-key-path           Path to the TLS certificate key, default: /opt/capsule-ns-filter/tls.key
```

## TLS Client Authentication
Users using a TLS client based authentication with certificate and key are able to talks with `capsule-ns-filter` since the current implementation of the reverse proxy is able to forward client certificates to the Kubernetes APIs server.

## OIDC Authentication
The `capsule-ns-filter` works with `kubectl` users with a token-based authentication, e.g. OIDC or Bearer Token.

In the following example, we'll use an OIDC server, e.g. [Keycloak](https://www.keycloak.org/), capable to provides JWT tokens.

### Configuring Keycloak
Configure Keycloak as OIDC server:

- Add a realm called `caas`, or use any existing realm instead
- Add a group `capsule.clastix.io`
- Add a user `alice` assigned to group `capsule.clastix.io`
- Add an OIDC client called `kubernetes`
- For the `kubernetes` client, create protocol mappers called `groups` and `audience`

If everything is done correctly, now you should be able to authenticate in Keycloak and see user groups in JWT tokens. Use the following snippet to authenticate in Keycloak as `alice` user:

```
$ KEYCLOAK=sso.clastix.io
$ REALM=caas
$ OIDC_ISSUER=${KEYCLOAK}/auth/realms/${REALM}

$ curl -k -s https://${OIDC_ISSUER}/protocol/openid-connect/token \
     -d grant_type=password \
     -d response_type=id_token \
     -d scope=openid \
     -d client_id=${OIDC_CLIENT_ID} \
     -d client_secret=${OIDC_CLIENT_SECRET} \
     -d username=${USERNAME} \
     -d password=${PASSWORD} | jq
```

The result will include an `ACCESS_TOKEN`, a `REFRESH_TOKEN`, and an `ID_TOKEN`. The access-token can generally be disregarded for Kubernetes. It would be used if the identity provider was managing roles and permissions for the users but that is done in Kubernetes itself with RBAC. The id-token is short lived while the refresh-token has longer expiration. The refresh-token is used to fetch a new id-token when the id-token expires.

```json
{  
   "access_token":"ACCESS_TOKEN",
   "refresh_token":"REFRESH_TOKEN",
   "id_token": "ID_TOKEN",
   "token_type":"bearer",
   "scope": "openid groups profile email"
   ...
}
```

To introspect the `ID_TOKEN` token run:
```
$ curl -k -s https://${OIDC_ISSUER}/protocol/openid-connect/introspect \
     -d token=${ID_TOKEN} \
     --user ${OIDC_CLIENT_ID}:${OIDC_CLIENT_SECRET} | jq
```

The result will be like the following:

```json
{
    ...
  "exp": 1601323086,
  "iat": 1601322186,
  "aud": "kubernetes",
  "typ": "ID",
  "azp": "kubernetes",
  "preferred_username": "alice",
  "email_verified": false,
  "acr": "1",
  "groups": [
    "capsule.clastix.io"
  ],
  "client_id": "kubernetes",
  "username": "alice",
  "active": true
    ...
}
```

### Configuring Kubernetes API Server
Configuring Kubernetes for OIDC Authentication requires adding several parameters to the API Server. Please, refer to the [documentation](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#openid-connect-tokens) for details and examples. Most likely, your `kube-apiserver.yaml` manifest will looks like the following:

```yaml
...
spec:
  containers:
  - command:
    - kube-apiserver
    ...
    - --oidc-issuer-url=https://${OIDC_ISSUER}
    - --oidc-ca-file=/etc/kubernetes/oidc/ca.crt
    - --oidc-client-id=${OIDC_CLIENT_SECRET}
    - --oidc-username-claim=preferred_username
    - --oidc-groups-claim=groups
    - --oidc-username-prefix=-
```

### Configuring Capsule
Make sure to have a working instance of the Capsule Operator in your Kubernetes cluster before to attempt to use `capsule-ns-filter`. Please, refer to the Capsule [documentation](https://github.com/clastix/capsule) for details and examples.

You should have one or more tenants defined, e.g. `oil` and `gas` and they are assigned to the user `alice`.

As cluster admin, check there are the tenants:

```
$ kubectl get tenants
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   AGE
foo    3                 1                 joe          User         4d
gas    3                 0                 alice        User         1d
oil    9                 0                 alice        User         1d
```

### Configuring kubectl
There are two options to use `kubectl` with OIDC:

- OIDC Authenticator
- Use the `--token` option

To use the OIDC Authenticator, add an `oidc` user entry to your `kubeconfig` file:
```
$ kubectl config set-credentials oidc \
    --auth-provider=oidc \
    --auth-provider-arg=idp-issuer-url=https://${OIDC_ISSUER} \
    --auth-provider-arg=idp-certificate-authority=/path/to/ca.crt \
    --auth-provider-arg=client-id=${OIDC_CLIENT_ID} \
    --auth-provider-arg=client-secret=${OIDC_CLIENT_SECRET} \
    --auth-provider-arg=refresh-token=${REFRESH_TOKEN} \
    --auth-provider-arg=id-token=${ID_TOKEN} \
    --auth-provider-arg=extra-scopes=groups
```

To use the --token option:
```
$ kubectl config set-credentials oidc --token=${ID_TOKEN}
```

Point the kubectl to the URL where the `capsule-ns-filter` service is reachable:
```
$ kubectl config set-cluster mycluster \
    --server=https://kube.clastix.io \
    --certificate-authority=~/.kube/ca.crt
```

Create a new context for the OIDC authenticated users:
```
$ kubectl config set-context alice-oidc@mycluster \
    --cluster=mycluster \
    --user=oidc
```

As user `alice`, you should be able to use `kubectl` to create some namespaces:
```
$ kubectl --context alice-oidc@mycluster create namespace oil-production
$ kubectl --context alice-oidc@mycluster create namespace oil-development
$ kubectl --context alice-oidc@mycluster create namespace gas-marketing
```

and list only those namespaces:
```
$ kubectl --context alice-oidc@mycluster get namespaces
NAME                STATUS   AGE
gas-marketing       Active   2m
oil-development     Active   2m
oil-production      Active   2m
```

When logged as cluster-admin power user you should be able to see all namespaces:
```
$ kubectl get namespaces
NAME                STATUS   AGE
default             Active   78d
kube-node-lease     Active   78d
kube-public         Active   78d
kube-system         Active   78d
gas-marketing       Active   2m
oil-development     Active   2m
oil-production      Active   2m
```

_Nota Bene_: once your `ID_TOKEN` expires, the `kubectl` OIDC Authenticator will attempt to refresh automatically your `ID_TOKEN` using the `REFRESH_TOKEN`, the `OIDC_CLIENT_ID` and the `OIDC_CLIENT_SECRET` storing the new values for the `REFRESH_TOKEN` and `ID_TOKEN` in your `kubeconfig` file.

In case the OIDC uses a self signed CA certificate, make sure to specify it with the `idp-certificate-authority` option in your `kubeconfig` file, otherwise you'll not able to refresh the tokens. Once the `REFRESH_TOKEN` is expired, you will need to refresh tokens manually.

## RBAC
The service account used for `capsule-ns-filter` needs to have `cluster-admin` permissions.

## Configuring client-only dashboards
If you're using a client-only dashboard, for example [Mirantis Lens](https://k8slens.dev/), the `capsule-ns-filter` can be used as in the previous `kubectl` example since Lens just needs for a `kubeconfig` file. Assuming to use a `kubeconfig` file containing a valid OIDC token released for the `alice` user, you can access the cluster with Lens dashboard and see only namespaces belonging to the Alice's tenants.

For web based dashboards, like the [Kubernetes Dashboard](https://github.com/kubernetes/dashboard), the `capsule-ns-filter` can be installed as sidecar container. See [Sidecar Installation](./sidecar.md).
