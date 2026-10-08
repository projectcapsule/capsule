---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "A ResourcePoolClaim requests more than its ResourcePool has left"
    kind: condition
    unattended: true
    entities:
      - { entity: resource-pool-claim, effect: reads, facts: [Claimed resources] }
      - { entity: resource-pool, effect: reads, facts: [Allocation] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product marks the claim exhausted and records the shortfall on the pool"
    kind: product
    entities:
      - { entity: resource-pool-claim, effect: changes, from: Unassigned, to: Exhausted, facts: [] }
      - { entity: resource-pool, effect: changes, facts: [Exhaustions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Hold a claim the pool cannot cover

## Trigger

A claim requests more than its pool has left.

## Outcome

The claim is exhausted and the pool records the shortfall.

## Edge cases

- With an ordered queue, later claims for an exhausted resource wait behind it; otherwise smaller later claims may still be allocated.
