---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes an exhausted ResourcePoolClaim"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: removes, from: Exhausted }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Delete a claim the pool cannot cover

## Trigger

A claim has waited too long for a pool that cannot cover it.

## Outcome

The claim is gone and no longer counts as a shortfall on the pool.
