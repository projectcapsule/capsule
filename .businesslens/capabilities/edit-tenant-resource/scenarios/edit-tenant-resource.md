---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner changes the items of a TenantResource"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: tenant-resource, effect: changes, facts: [Resources] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Change what a replication copies

## Trigger

A tenant owner needs different objects, or the same objects in other namespaces of the tenant.

## Outcome

Capsule then replicates as the TenantResource now says: the selected namespaces hold the objects the TenantResource now describes; copies it no longer produces are removed or left in place as their deletion policy says.

## Edge cases

- A lifecycle condition expression that does not compile is refused.
- A reference to a namespace outside the tenant stops the replication, as when it is created.
