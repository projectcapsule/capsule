# Kubernetes multi-tenancy made simple
**Capsule** helps to implement a multi-tenancy and policy-based environment in your Kubernetes cluster. It is not intended to be yet another _PaaS_, instead, it has been designed as a micro-services based ecosystem with minimalist approach, leveraging only on upstream Kubernetes. 

# What's the problem with the current status?
Kubernetes introduces the _Namespace_ object type to create logical partitions of the cluster as isolated *slices*. However, implementing advanced multi-tenancy scenarios, it becomes soon complicated because of the flat structure of Kubernetes namespaces and the impossibility to share resources among namespaces belonging to the same tenant. To overcome this, cluster admins tend to provision a dedicated cluster for each groups of users, teams, or departments. As an organization grows, the number of clusters to manage and keep aligned becomes an operational nightmare, described as the well know phenomena of the _clusters sprawl_.

# Entering Caspule
Capsule takes a different approach. In a single cluster, it aggregates multiple namespaces in a lightweight abstraction called _Tenant_. Within each tenant, users are free to create their namespaces and share all the assigned resources while a Policy Engine keeps different tenants isolated from each other. The _Network and Security Policies_, _Resource Quota_, _Limit Ranges_, _RBAC_, and other policies defined at the tenant level are automatically inherited by all the namespaces in the tenant. And users are free to operate their tenants in authonomy, without the intervention of the cluster administrator.

# Features
## Self-Service
Leave to developers the freedom to self-provision their cluster resources according to the assigned boundaries.

## Preventing Clusters Sprawl
Share a single cluster with multiple teams, groups of users, or departments by saving operational and management efforts.

## Governance
Leverage Kubernetes Admission Controllers to enforce the industry security best practices and meet legal requirements.

## Resources Control
Take control of the resources consumed by users while preventing them to overtake.

## Native Experience
Provide multi-tenancy with a native Kubernetes experience without introducing additional management layers, plugins, or customised binaries.

## GitOps ready
Capsule is completely declarative and GitOps ready.

## Bring your own device (BYOD)
Assign to tenants a dedicated set of compute, storage, and network resources and avoid the noisy neighbors' effect.

# Common use cases for Capsule
Please, refer to the corresponding [section](./docs/operator/use-cases/overview.md) in the project documentation for a detailed list of common use cases that Capsule can address.

# What’s next
Have a fun with Capsule:

* [Getting Started](./getting-started.md)
* [Use Cases](./use-cases/overview.md)
* [Contributing](./contributing.md)
* [References](./references.md)