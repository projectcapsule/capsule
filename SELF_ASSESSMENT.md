# Capsule Security Self-Assessment

## Metadata
<table>
  <tr>
   <td>Software
   </td>
   <td><a href="https://github.com/projectcapsule/capsule">https://github.com/projectcapsule/capsule</a>
   </td>
  </tr>
  <tr>
   <td>Website
   </td>
   <td><a href="https://capsule.clastix.io/">https://capsule.clastix.io/</a>
   </td>
  </tr>
  <tr>
   <td>Security Provider
   </td>
   <td>No
   </td>
  </tr>
  <tr>
   <td>Languages
   </td>
   <td>Golang
   </td>
  </tr>
  <tr>
   <td>SBOM
   </td>
   <td><a href="https://github.com/projectcapsule/capsule/pkgs/container/sbom">https://github.com/projectcapsule/capsule/pkgs/container/sbom</a>
   </td>
  </tr>
</table>

## Security Links

<table>
  <tr>
   <td><strong>Doc</strong>
   </td>
   <td><strong>URL</strong>
   </td>
  </tr>
  <tr>
   <td>Security file
   </td>
   <td><a href="https://github.com/projectcapsule/capsule/blob/main/SECURITY.md">https://github.com/projectcapsule/capsule/blob/main/SECURITY.md</a>
   </td>
  </tr>
  <tr>
   <td>Default and optional configs
   </td>
   <td><a href="https://github.com/projectcapsule/capsule/blob/main/charts/capsule/values.yaml">https://github.com/projectcapsule/capsule/blob/main/charts/capsule/values.yaml</a>
   </td>
  </tr>
</table>

## Overview

Capsule implements a multi-tenant and policy-based environment in your Kubernetes cluster.
It is designed as a micro-services-based ecosystem with a minimalist approach, leveraging only upstream Kubernetes.

### Background

Capsule takes a different approach.
In a single cluster, the Capsule Controller aggregates multiple namespaces in a lightweight abstraction called Tenant, basically a grouping of Kubernetes Namespaces.
Within each tenant, users are free to create their namespaces and share all the assigned resources.

On the other side, the Capsule Policy Engine keeps the different tenants isolated from each other.
Network and Security Policies, Resource Quota, Limit Ranges, RBAC, and other policies defined at the tenant level are automatically inherited by all the namespaces in the tenant.
Then users are free to operate their tenants in autonomy, without the intervention of the cluster administrator.

Capsule was accepted as a CNCF sandbox project in December 2022.

## Actors

### Capsule Operator

It's the Operator which provides all the multi-tenant capabilities offered by Capsule.
It's made of two internal components, such as the webhooks server (known as _policy engine_), and the _tenant controller_.

**Capsule Tenant Controller** 

The controller is responsible for managing the tenants by reconciling the required objects at the Namespace level, such as _Network Policy_, _LimitRange_, _ResourceQuota_, _Role Binding_, as well as labelling the Namespace objects belonging to a Tenant according to their desired metadata.
It is responsible for binding Namespaces to the selected Tenant, and managing their lifecycle.

Furthermore, the manager can replicate objects thanks to the **Tenant Resource** API, which offers two levels of interactions: a cluster-scoped one thanks to the `GlobalTenantResource` API, and a namespace-scoped one named `TenantResource`.

The replicated resources are dynamically created, and replicated by Capsule itself, as well as preserving the deletion of these objects by the Tenant owner.

**Capsule Tenant Controller (Policy Engine)** 

Policies are defined on a Tenant basis: therefore the policy engine is enforcing these policies on the tenants's Namespaces and their children's resources.
The Policy Engine is currently not a dedicated component, but a part of the Capsule Tenant Controller. 

The webhook server, also known as the policy engine, interpolates the Tenant rules and takes full advantage of the dynamic admission controllers offered by Kubernetes itself (such as `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration`).
Thanks to the _policy engine_ the cluster administrators can enforce specific rules such as preventing _Pod_ objects from untrusted registries to run or preventing the creation of _PersistentVolumeClaim_ resources using a non-allowed _StorageClass_, etc.

It also acts as a defaulter webhook, offloading the need to specify some classes (IngressClass, StorageClass, RuntimeClass, etc.).

### Capsule Proxy

The `capsule-proxy` is an addon which is offering a Kubernetes API Server shim aware of the multi-tenancy levels implemented by Capsule.

It's essentially a reverse proxy that decorates the incoming requests with the required `labelSelector` query string parameters to filter out some objects, such as:

- Namespaces
- IngressClass
- StorageClass
- PriorityClass
- RuntimeClass
- PersistentVolumes

Permissions on those resources are not enforced through the classic Kubernetes RBAC but rather at the Tenant Owner level, allowing fine-grained control over specific resources.

`capsule-proxy` is not serving itself Kubernetes API server responses, but rather, it's acting as a middle proxy server offering dynamic filtering of requests: this means the resulting responses from the upstream are not mangled, and don't require any additional plugins, or third-party binaries, and integrating with any external components, such as the Kubernetes dashboard, the `kubectl` binary, etc.

## Actions

Tenants are created by cluster administrators, who have the right to create Tenant custom resource instances.
End users should not manage tenants.
Therefore users without any cluster administration rights can't list tenants or create tenants.

## Creating namespaces in a tenant

When creating a tenant, the Capsule controller inspects the user's supplied groups and matches, if the groups were defined in the `capsuleConfiguration`. If at least one matching group is found, the user request is considered by Capsule. If not, Capsule ignores the request and does not perform any action. This also applies to modifications (`UPDATE` request).

To create namespaces within a tenant a User, Group or ServiceAccount must be configured as owner of the tenant.
A namespace is assigned to a tenant based on its label, its owner (if the owner only has one tenant) or the prefix of the namespace (which matches the tenant name).

If the request is considered, the namespace is created with all the configuration on the tenant, which is relevant. The additional resources (NetworkPolicy, ResourceQuota, LimitRange) are created in the new namespace.

### Applying Workload and configs to namespaces within a tenant

Whenever a tenant user applies new workloads or configs to a namespace, the capsule controller inspects the namespace and checks if it belongs to a tenant. If so, the capsule controller applies policies from the tenant configuration to the given workloads and configs.

If there are defaults defined on the tenant, they are applied to the workloads as well.
This is a further abstraction from having cluster defaults (eg. default `StorageClass`) to having tenant defaults (eg. default `StorageClass` for a tenant).

### Goals

**General**

* **Multitenancy**: Capsule should be able to support multiple tenants in a single Kubernetes cluster without introducing overhead or cognitive load, and barely relying on Namespace objects.
* **Kubernetes agnostic**: Capsule should integrate with Kubernetes primitives, such as _RBAC_, _NetworkPolicy_, _LimitRange_, and _ResourceQuota_.
* **Policy-based**: Capsule should be able to enforce policies on tenants, which are defined on a tenant basis.
* **Native User Experience**: Capsule shouldn't increase the cognitive load of developers, such as introducing `kubectl` plugins, or forcing the tenant owners to operate their tenant objects using Custom Resource Definitions.

### Non-Goals

**General**

* **Control Plane**: Capsule can't mimic for each tenant a feeling of a dedicated control plane. 

* **Custom Resource Definitions**: Capsule doesn't want to provide virtual cluster capabilities and it's sticking to the native Kubernetes user experience and design; rather, its focus is to provide a governance solution by focusing on resource optimization and security lockdown.


## Self-assessment use

This self-assessment is created by the Capsule team to perform an internal analysis of the project's security.
It is not intended to provide a security audit of Capsule, or function as an independent assessment or attestation of Capsule’s security health.

This document serves to provide Capsule users with an initial understanding of Capsule's security, where to find existing security documentation, Capsule plans for security, and a general overview of Capsule security practices, both for the development of Capsule as well as security of Capsule.

This document provides the CNCF TAG-Security with an initial understanding of Capsule to assist in a joint review, necessary for projects under incubation. Taken together, this document and the joint review serve as a cornerstone for if and when Capsule seeks graduation.

## Security functions and features

See [Actors](#actors) and [Actions](#actions) for a more detailed description of the critical actors, actions, and potential threats.

## Project compliance

As of now, not applicable.

## Secure development practices

The Capsule project follows established CNCF and OSS best practices for code development and delivery.
Capsule follows [OpenSSF Best Practices](https://www.bestpractices.dev/en/projects/5601).
Although not perfect yet, we are constantly trying to improve and score optimal scores.
We will assess the issues during our community meetings and try to plan them for future releases.

### Development Pipeline

Changes must be reviewed and merged by the project maintainers.
Before changes are merged, all the changes must pass static checks, license checks, verifications on `gofmt`, `go lint`, `go vet`, and pass all unit tests and e2e tests.
Changes are scanned by trivy for the docker images.
We run E2E tests for different Kubernetes versions on Pull Requests.
Code changes are submitted via Pull Requests (PRs) and must be signed and verified.
Commits to the main branch directly are not allowed.

## Security issue resolution

Capsule project vulnerability handling related processes are recorded in the [Capsule Security Doc](https://github.com/projectcapsule/capsule/blob/main/SECURITY.md).
Related security vulnerabilities can be reported and communicated via email to [cncf-capsule-maintainers@lists.cncf.io](mailto:cncf-capsule-maintainers@lists.cncf.i).

## Appendix

All Capsule security-related issues (both fixes and enhancements) are labelled with "security" and can be queried using[ https://github.com/projectcapsule/capsule/labels/security](https://github.com/projectcapsule/capsule/labels/security).
The code review process requires maintainers to consider security while reviewing designs and pull requests.
