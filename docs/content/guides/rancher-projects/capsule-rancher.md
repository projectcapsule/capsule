# Capsule and Rancher Projects

This guide explains how to setup the integration between Capsule and Rancher Projects.

It then explains how for the tenant user, the access to Kubernetes resources is transparent.

## Manually

## Pre-requisites

- An authentication provider in Rancher, e.g. an OIDC identity provider
- A *Tenant Member* `Cluster Role` in Rancher

### Configure an identity provider for Kubernetes

You can follow [this general guide](https://capsule.clastix.io/docs/guides/oidc-auth) to configure an OIDC authentication for Kubernetes.

For a Keycloak specific setup yon can check [this resources list](./oidc-keycloak.md).

#### Known issues

##### Keycloak new URLs without `/auth` makes Rancher crash

- [rancher/rancher#38480](https://github.com/rancher/rancher/issues/38480)
- [rancher/rancher#38683](https://github.com/rancher/rancher/issues/38683)

### Create the Tenant Member Cluster Role

A custom Rancher `Cluster Role` is needed to allow Tenant users, to read cluster-scope resources and Rancher doesn't provide e built-in Cluster Role with this tailored set of privileges.

When logged-in to the Rancher UI as administrator, from the Users & Authentication page, create a Cluster Role named *Tenant Member* with the following privileges:
- `get`, `list`, `watch` operations over `IngressClasses` resources.
- `get`, `list`, `watch` operations over `StorageClasses` resources.
- `get`, `list`, `watch` operations over `PriorityClasses` resources.
- `get`, `list`, `watch` operations over `Nodes` resources.
- `get`, `list`, `watch` operations over `RuntimeClasses` resources.

## Configuration (administration)

### Tenant onboarding

When onboarding tenants, the administrator needs to create the following, in order to bind the `Project` with the `Tenant`:

- In Rancher, create a `Project`.
- In the target Kubernetes cluster, create a `Tenant`, with the following specification:
  ```yaml
  kind: Tenant
  ...
  spec:
    namespaceOptions:
      additionalMetadata:
        annotations:
          field.cattle.io/projectId: ${CLUSTER_ID}:${PROJECT_ID}
        labels:
          field.cattle.io/projectId: ${PROJECT_ID}
  ```
  where `$CLUSTER_ID` and `$PROEJCT_ID` can be retrieved, assuming a valid `$CLUSTER_NAME`, as:

  ```shell
  CLUSTER_NAME=foo
  CLUSTER_ID=$(kubectl get cluster -n fleet-default ${CLUSTER_NAME} -o jsonpath='{.status.clusterName}')
  PROJECT_IDS=$(kubectl get projects -n $CLUSTER_ID -o jsonpath="{.items[*].metadata.name}")
  for project_id in $PROJECT_IDS; do echo "${project_id}"; done
  ```

  More on declarative `Project`s [here](https://github.com/rancher/rancher/issues/35631).
- In the identity provider, create a user with [correct OIDC claim](https://capsule.clastix.io/docs/guides/oidc-auth) of the Tenant.
- In Rancher, add the new user to the `Project` with the *Read-only* `Role`.
- In Rancher, add the new user to the `Cluster` with the *Tenant Member* `Cluster Role`.

#### Create the Tenant Member Project Role

A custom `Project Role` is needed to allow Tenant users, with minimun set of privileges and create and delete `Namespace`s.

Create a Project Role named *Tenant Member* that inherits the privileges from the following Roles:
- *read-only*
- *create-ns*


### Usage

When the configuration administrative tasks have been completed, the tenant users are ready to use the Kubernetes cluster transparently.

For example can create Namespaces in a self-service mode, that would be otherwise impossible with the sole use of Rancher Projects.

#### Namespace creation

From the tenant user perspective both CLI and the UI are valid interfaces to communicate with.

#### From CLI

- Tenants `kubectl`-logs in to the OIDC provider
- Tenant creates a Namespace, as a valid OIDC-discoverable user.

the `Namespace` is now part of both the Tenant and the Project.

> As administrator, you can verify with:
>
> ```shell
> kubectl get tenant ${TENANT_NAME} -o jsonpath='{.status}'
> kubectl get namespace -l field.cattle.io/projectId=${PROJECT_ID}
> ```

#### From UI

- Tenants logs in to Rancher, with a valid OIDC-discoverable user (in a valid Tenant group).
- Tenant user create a valid Namespace

the `Namespace` is now part of both the Tenant and the Project.

> As administrator, you can verify with:
>
> ```shell
> kubectl get tenant ${TENANT_NAME} -o jsonpath='{.status}'
> kubectl get namespace -l field.cattle.io/projectId=${PROJECT_ID}
> ```

### Additional administration

#### Project monitoring

Before proceeding is recommended to read the official Rancher documentation about [Project Monitors](https://ranchermanager.docs.rancher.com/v2.6/how-to-guides/advanced-user-guides/monitoring-alerting-guides/prometheus-federator-guides/project-monitors).

In summary, the setup is composed by a cluster-level Prometheus, Prometheus Federator via which single Project-level Prometheus federate to.

#### Network isolation

Before proceeding is recommended to read the official Capsule documentation about [`NetworkPolicy` at `Tenant`-level](https://capsule.clastix.io/docs/general/tutorial/#assign-network-policies)`.

##### Network isolation and Project Monitor

As Rancher's Project Monitor deploys the Prometheus stack in a `Namespace` that is not part of **neither** the `Project` **nor** the `Tenant` `Namespace`s, is important to apply the label selectors in the `NetworkPolicy` `ingress` rules to the `Namespace` created by Project Monitor.

That Project monitoring `Namespace` will be named as `cattle-project-<PROJECT_ID>-monitoring`.

For example, if the `NetworkPolicy` is configured to allow all ingress traffic from `Namespace` with label `capsule.clastix.io/tenant=foo`, this label is to be applied to the Project monitoring `Namespace` too.

Then, a `NetworkPolicy` can be applied at `Tenant`-level with Capsule `GlobalTenantResource`s. For example it can be applied a minimal policy for the *oil* `Tenant`:

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: GlobalTenantResource
metadata:
  name: oil-networkpolicies
spec:
  tenantSelector:
    matchLabels:
      capsule.clastix.io/tenant: oil
  resyncPeriod: 360s
  pruningOnDelete: true
  resources:
    - namespaceSelector:
        matchLabels:
          capsule.clastix.io/tenant: oil
      rawItems:
      - apiVersion: networking.k8s.io/v1
        kind: NetworkPolicy
        metadata:
          name: oil-minimal
        spec:
          podSelector: {}
          policyTypes:
            - Ingress
            - Egress
          ingress:
            # Intra-Tenant
            - from:
              - namespaceSelector:
                  matchLabels:
                    capsule.clastix.io/tenant: oil
            # Rancher Project Monitor stack
            - from:
              - namespaceSelector:
                  matchLabels:
                    role: monitoring
			# Kubernetes nodes
            - from:
              - ipBlock:
                  cidr: 192.168.1.0/24
          egress:
            # Kubernetes DNS server
            - to:
              - namespaceSelector: {}
                podSelector:
                  matchLabels:
                    k8s-app: kube-dns
                ports:
                  - port: 53
                    protocol: UDP
            # Intra-Tenant
            - to:
              - namespaceSelector:
                  matchLabels:
                    capsule.clastix.io/tenant: oil
            # Kubernetes API server
            - to:
              - ipBlock:
                  cidr: 10.43.0.1/32
                ports:
                  - port: 443
```

## Cluster-wide resources and Rancher Shell interface

For using the Rancher Shell and cluster-wide resources as tenant user, please follow [this guide](./capsule-proxy-rancher.md).


