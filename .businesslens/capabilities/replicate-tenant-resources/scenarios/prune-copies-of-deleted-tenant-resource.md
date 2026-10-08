---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "A TenantResource is being deleted"
    kind: condition
    unattended: true
    entities:
      - { entity: tenant-resource, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product removes the replicated resources it created, leaving in place those whose deletion policy keeps them"
    kind: product
    entities:
      - { entity: replicated-resource, effect: removes }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Remove copies when a TenantResource is deleted

## Trigger

A TenantResource is being deleted.

## Outcome

The objects it created are removed, except those whose deletion policy keeps them.

## Edge cases

- An object that cannot be removed keeps the TenantResource from being deleted.
