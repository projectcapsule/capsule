---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "A TenantResource changes, its resync period passes, or a namespace of its Tenant changes"
    kind: condition
    unattended: true
    entities:
      - { entity: tenant-resource, effect: reads, facts: [Resync period] }
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product renders the items of the TenantResource for each selected namespace of its Tenant, acting as the identity it names"
    kind: product
    entities:
      - { entity: tenant-resource, effect: reads, facts: [Resources, Service account] }
      - { entity: namespace, effect: reads, facts: [Labels] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product applies each replicated resource and removes those it no longer produces according to their deletion policy"
    kind: product
    entities:
      - { entity: replicated-resource, as: copy, effect: creates, facts: [Origin, Protected, Deletion policy] }
      - { entity: replicated-resource, as: stale-copy, effect: removes }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product records the outcome of every item on the TenantResource"
    kind: product
    entities:
      - { entity: tenant-resource, effect: changes, facts: [Processed items, Ready] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Replicate into a tenant's namespaces

## Trigger

A TenantResource changes, its resync period passes, or a namespace of its tenant changes.

## Outcome

Every selected namespace holds the replicated objects, and the TenantResource reports each item's outcome.

## Edge cases

- A namespace created in the tenant receives the replicated objects at once.
- A cordoned TenantResource applies and removes nothing.
- A TenantResource waits until the TenantResources it depends on are ready.
- Cluster-scoped kinds are refused.
- An item whose lifecycle condition is false is skipped without being removed.
