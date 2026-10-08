---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner marks a ResourcePoolClaim in use for release"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: changes, facts: [Release] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product takes the claim out of the pool and the namespace quota and returns it to unassigned"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: changes, from: In use, to: Unassigned, facts: [Release] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Release a claim in use

## Trigger

A tenant owner wants to free a claim that usage still needs, for example to resize it.

## Outcome

The claim is unassigned and can be changed; its amounts no longer count in the pool or the namespace quota.
