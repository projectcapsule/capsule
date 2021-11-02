# Standalone Installation
The `capsule-proxy` can be deployed in standalone mode, e.g. running as a pod bridging any Kubernetes client to the `kube-apiserver`. Use this way to provide access to client-side command line tools like `kubectl` or even client-side dashboards.

You can use an Ingress Controller to expose the `capsule-proxy` endpoint in SSL passthrough, or,depending on your environment, you can expose it with either a `NodePort`, or a `LoadBalancer` service. As further alternatives, use `HostPort` or `HostNetwork` mode.

```
                +-----------+          +-----------+         +-----------+
 kubectl ------>|:443       |--------->|:9001      |-------->|:6443      |
                +-----------+          +-----------+         +-----------+
                ingress-controller     capsule-proxy         kube-apiserver
                (ssl-passthrough)
``` 

## Configure Capsule
Make sure to have a working instance of the Capsule Operator in your Kubernetes cluster before to attempt to use `capsule-proxy`. Please, refer to the Capsule Operator [documentation](/docs/operator/overview) for instructions.

You should also have one or more tenants defined, e.g. `oil` and `gas` and they are assigned to the user `alice`.

As cluster admin, check there are the tenants:

```
$ kubectl get tenants
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   AGE
foo    3                 1                 joe          User         4d
gas    3                 0                 alice        User         1d
oil    9                 0                 alice        User         1d
```

## Install Capsule Proxy
Create a secret in the target namespace containing the SSL certificate which `capsule-proxy` will use.

```
$ kubectl -n capsule-system create secret tls capsule-proxy --cert=tls.cert --key=tls.key
```

Then use the Helm Chart to install the `capsule-proxy` in such namespace:

```bash
$ cat <<EOF | sudo tee custom-values.yaml
options:
  enableSSL: true
ingress:
  enabled: true
  annotations:
    ingress.kubernetes.io/ssl-passthrough: 'true'
  hosts:
    - host: kube.clastix.io
      paths: [ "/" ]
EOF

$ helm install capsule-proxy clastix/capsule-proxy \
  --values custom-values.yaml \
  -n capsule-system
```

The `capsule-proxy` should be exposed with an Ingress in SSL passthrough mode and reachable at `https://kube.clastix.io`.

Users using a TLS client based authentication with certificate and key are able to talks with `capsule-proxy` since the current implementation of the reverse proxy is able to forward client certificates to the Kubernetes APIs server.

## RBAC Considerations
Currently, the service account used for `capsule-proxy` needs to have `cluster-admin` permissions.

## Configuring client-only dashboards
If you're using a client-only dashboard, for example [Lens](https://k8slens.dev/), the `capsule-proxy` can be used as in the previous `kubectl` example since Lens just needs for a `kubeconfig` file. Assuming to use a `kubeconfig` file containing a valid OIDC token released for the `alice` user, you can access the cluster with Lens dashboard and see only namespaces belonging to the Alice's tenants.

For web based dashboards, like the [Kubernetes Dashboard](https://github.com/kubernetes/dashboard), the `capsule-proxy` can be installed as sidecar container. See [Sidecar Installation](/docs/proxy/sidecar).
