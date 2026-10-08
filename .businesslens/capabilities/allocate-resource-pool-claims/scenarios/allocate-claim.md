---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "A ResourcePoolClaim, its ResourcePool or usage in a selected namespace changes"
    kind: condition
    unattended: true
    entities:
      - { entity: resource-pool-claim, effect: reads, facts: [] }
      - { entity: resource-pool, effect: reads, facts: [] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product allocates waiting claims oldest first while the pool has room for every claimed resource"
    kind: product
    entities:
      - { entity: resource-pool-claim, effect: changes, from: Unassigned, to: Allocated, facts: [] }
      - { entity: resource-pool, effect: changes, facts: [Allocation] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product sets the pool quota of each namespace to its allocated claims plus the pool defaults"
    kind: product
    entities:
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: resource-pool, effect: reads, facts: [Defaults] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product marks as in use the claims, oldest first, that current usage in the namespace needs"
    kind: product
    entities:
      - { entity: resource-pool-claim, effect: changes, from: Allocated, to: In use, facts: [] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Allocate claims and track their use

## Trigger

A claim, its pool or usage in a selected namespace changes.

## Outcome

Claims that fit are allocated oldest first; the namespace quota grows by them; claims current usage needs are in use.

## Edge cases

- A claim no longer needed by usage returns from in use to allocated.
