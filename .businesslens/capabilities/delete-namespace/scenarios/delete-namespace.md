---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes a namespace of their Tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: changes, from: Active, to: Terminating, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product drops it from the Tenant once Kubernetes has removed it"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Namespaces] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Delete a namespace

## Trigger

A tenant owner no longer needs a namespace.

## Outcome

The namespace terminates; once Kubernetes has removed it, it no longer counts towards the Tenant's namespace quota.

## Edge cases

- While a namespace is terminating, the tenant it belongs to cannot be changed, and ResourcePermits and objects Capsule protects in it can be deleted.
