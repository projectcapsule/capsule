---
domain: tenants
relations:
  - { entity: service-account, verb: contains, cardinality: one-to-many }
  - { entity: workload, verb: contains, cardinality: one-to-many }
  - { entity: service, verb: contains, cardinality: one-to-many }
  - { entity: ingress, verb: contains, cardinality: one-to-many }
  - { entity: gateway, verb: contains, cardinality: one-to-many }
  - { entity: persistent-volume-claim, verb: contains, cardinality: one-to-many }
  - { entity: network-policy, verb: contains, cardinality: one-to-many }
  - { entity: resource-claim, verb: contains, cardinality: one-to-many }
  - { entity: tenant-resource, verb: contains, cardinality: one-to-many }
  - { entity: resource-pool-claim, verb: contains, cardinality: one-to-many }
  - { entity: resource-permit, verb: contains, cardinality: one-to-many }
  - { entity: custom-quota, verb: contains, cardinality: one-to-many }
  - { entity: resource-permit-template, verb: contains, cardinality: one-to-many }
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/mutation/assignment.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenant/namespaces.go" }
---

# Namespace

A Kubernetes namespace that belongs to a tenant. Capsule assigns it when it is created and keeps the tenant's policy, role bindings and rules applied to it.

## Information kept

- **Name** — the namespace name
- **Tenant** — the tenant it belongs to
- **Labels** — its labels
- **Annotations** — its annotations
- **Managed metadata** — labels and annotations Capsule keeps on it from its tenant and rules, including the node selector and the cordoned marker
- **Role bindings** — the cluster roles bound in it for owners, additional role bindings, rule permissions and promotions
- **Effective rules** — the ordered namespace rules that select it, rendered for it

## States

### Active

The namespace is in use and follows its tenant's policy.

### Terminating

Kubernetes is deleting the namespace and its content; its tenant can no longer change.
