# Capsule Proxy and Rancher Projects

This guide explains how to setup the integration between Capsule Proxy and Rancher Projects.

It then explains how for the tenant user, the access to Kubernetes cluster-wide resources is transparent.

## Rancher Shell and Capsule

In order to integrate the Rancher Shell with Capsule it's needed to route the Kubernetes API requests made from the shell, via Capsule Proxy.

The [capsule-rancher-addon](https://github.com/clastix/capsule-addon-rancher/tree/master/charts/capsule-rancher-addon) allows the integration transparently.

### Install the Capsule addon

Add the Clastix Helm repository `https://clastix.github.io/charts`.

By updating the cache with Clastix's Helm repository a Helm chart named `capsule-rancher-addon` is available.

Install keeping attention to the following Helm values:

* `proxy.caSecretKey`: the `Secret` key that contains the CA certificate used to sign the Capsule Proxy TLS certificate (it should be`"ca.crt"` when Capsule Proxy has been configured with certificates generated with Cert Manager).
* `proxy.servicePort`: the port configured for the Capsule Proxy Kubernetes `Service` (`443` in this setup).
* `proxy.serviceURL`: the name of the Capsule Proxy `Service` (by default `"capsule-proxy.capsule-system.svc"` hen installed in the *capsule-system* `Namespace`).

## Rancher Cluster Agent

In both CLI and dashboard use cases, the [Cluster Agent](https://ranchermanager.docs.rancher.com/v2.5/how-to-guides/new-user-guides/kubernetes-clusters-in-rancher-setup/launch-kubernetes-with-rancher/about-rancher-agents) is responsible for the two-way communication between Rancher and the downstream cluster.

In a standard setup, the Cluster Agents communicates to the API server. In this setup it will communicate with Capsule Proxy to ensure filtering of cluster-scope resources, for Tenants.

Cluster Agents accepts as arguments:
- `KUBERNETES_SERVICE_HOST` environment variable
- `KUBERNETES_SERVICE_PORT` environment variable

which will be set, at cluster import-time, to the values of the Capsule Proxy `Service`. For example:
- `KUBERNETES_SERVICE_HOST=capsule-proxy.capsule-system.svc`
- (optional) `KUBERNETES_SERVICE_PORT=9001`. You can skip it by installing Capsule Proxy with Helm value `service.port=443`.

The expected CA is the one for which the certificate is inside the `kube-root-ca` `ConfigMap` in the same `Namespace` of the Cluster Agent (*cattle-system*).

## Capsule Proxy

Capsule Proxy needs to provide a x509 certificate for which the root CA is trusted by the Cluster Agent.
The goal can be achieved by, either using the Kubernetes CA to sign its certificate, or by using a dedicated root CA.

### With the Kubernetes root CA

> Note: this can be achieved when the Kubernetes root CA keypair is accessible. For example is likely to be possibile with on-premise setup, but not with managed Kubernetes services.

With this approach Cert Manager will sign certificates with the Kubernetes root CA for which it's needed to be provided a `Secret`.

```shell
kubectl create secret tls -n capsule-system kubernetes-ca-key-pair --cert=/path/to/ca.crt --key=/path/to/ca.key
```

When installing Capsule Proxy with Helm chart, it's needed to specify to generate Capsule Proxy `Certificate`s with Cert Manager with an external `ClusterIssuer`:
- `certManager.externalCA.enabled=true`
- `certManager.externalCA.secretName=kubernetes-ca-key-pair`
- `certManager.generateCertificates=true`

and disable the job for generating the certificates without Cert Manager:
- `options.generateCertificates=false`

### Enable tenant users access cluster resources

In order to allow tenant users to list cluster-scope resources, like `Node`s, Tenants need to be configured with proper `proxySettings`, for example:

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
    proxySettings:
    - kind: Nodes
      operations:
      - List
[...]
```

Also, in order to assign or filter nodes per Tenant, it's needed labels on node in order to be selected:

```shell
kubectl label node worker-01 capsule.clastix.io/tenant=oil
```

 and a node selector at Tenant level:

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  nodeSelector:
    capsule.clastix.io/tenant: oil
[...]
```

The final manifest is:

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
    proxySettings:
    - kind: Node
      operations:
      - List
  nodeSelector:
    capsule.clastix.io/tenant: oil
```

The same appplies for:
- `Nodes`
- `StorageClasses`
- `IngressClasses`
- `PriorityClasses`

More on this in the [official documentation](https://capsule.clastix.io/docs/general/proxy#tenant-owner-authorization).
