---
domain: tenants
relations:
  - { entity: namespace, verb: groups, cardinality: one-to-many }
  - { entity: namespace-rule, verb: has, cardinality: one-to-many }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenant_types.go#TenantSpec" }
  - { kind: code, role: implementation, target: "api/v1beta2/tenant_status.go" }
---

# Tenant

A group of namespaces that belongs to one set of owners, together with the policy every namespace in it inherits. Tenant owners create namespaces and work in them on their own; the policy keeps each tenant isolated from the others.

## Information kept

- **Name** — the tenant name, also the prefix of its namespace names when the prefix is forced
- **Owners** — the users, groups and ServiceAccounts listed as owners, each with the cluster roles it receives in every namespace
- **Owner selectors** — label selectors that pull matching TenantOwners in as owners
- **Owner promotion** — whether ServiceAccounts in the tenant may be promoted to owner
- **Effective owners** — listed owners, matched TenantOwners, administrators and ServiceAccounts promoted to owner
- **Namespace quota** — the most namespaces the tenant may hold; unlimited when unset
- **Namespaces** — the namespaces that belong to the tenant
- **Force tenant prefix** — whether namespace names must start with the tenant name and a dash; falls back to the CapsuleConfiguration
- **Namespace metadata** — labels and annotations given to every namespace, and the namespace metadata that is required or forbidden
- **Managed metadata only** — whether namespace labels and annotations are replaced by the ones Capsule manages
- **Node selector** — node labels that pods in every tenant namespace are scheduled to
- **Additional role bindings** — cluster roles bound to further subjects in every tenant namespace
- **Cordoned** — whether the tenant is frozen
- **Prevent deletion** — whether deleting the tenant is refused
- **Storage classes** — storage classes PersistentVolumeClaims may use, and the default one
- **Ingress options** — allowed ingress classes and hostnames, the default class, the hostname collision scope and whether wildcard hostnames are allowed
- **Gateway classes** — gateway classes Gateways may use, and the default one
- **Priority classes** — priority classes pods may use, and the default one
- **Runtime classes** — runtime classes pods may use, and the default one
- **Device classes** — device classes ResourceClaims may request
- **Service options** — Service types, external IPs and metadata allowed or forbidden for Services, and metadata added to them
- **Container registries** — registries container images may come from
- **Image pull policies** — image pull policies containers may use
- **Pod metadata** — labels and annotations added to every pod
- **Resource quotas** — resource quotas applied per namespace or across the whole tenant
- **Limit ranges** — limit ranges placed in every namespace
- **Network policies** — network policies placed in every namespace
- **Data** — free-form data that templates in rules and replications can read
- **Available classes** — the storage, priority, runtime, gateway and device classes the tenant may use
- **Promotions** — ServiceAccounts promoted through rules and the namespaces they act in

## States

### Active

Tenant owners create and change namespaces and resources.

### Cordoned

Tenant owners cannot create, change or delete namespaces or anything inside them.

### Terminating

The tenant is being deleted and accepts no new namespaces; its namespaces are being deleted.
