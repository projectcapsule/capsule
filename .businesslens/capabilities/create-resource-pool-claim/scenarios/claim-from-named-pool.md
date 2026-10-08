---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner creates a ResourcePoolClaim for an amount of a named ResourcePool"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: creates, to: Unassigned, facts: [Pool, Claimed resources, Release] }
      - { entity: resource-pool, effect: reads, facts: [] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Claim from a named pool

## Trigger

A namespace needs more resources than its defaults.

## Outcome

The claim waits to be allocated from the pool.
