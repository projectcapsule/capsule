---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner creates a ResourcePoolClaim without naming a pool"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: reads, facts: [Claimed resources] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product picks a ResourcePool that selects the namespace and offers every claimed resource, preferring one with room for it"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: creates, to: Unassigned, facts: [Pool, Claimed resources, Release] }
      - { entity: resource-pool, effect: reads, facts: [Namespace selectors, Quota, Allocation] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Claim without naming a pool

## Trigger

A tenant owner claims resources without knowing which pool offers them.

## Outcome

The claim names a pool that selects the namespace and offers every claimed resource, and waits to be allocated.

## Edge cases

- When no pool offers every claimed resource, the claim names no pool and is not allocated.
