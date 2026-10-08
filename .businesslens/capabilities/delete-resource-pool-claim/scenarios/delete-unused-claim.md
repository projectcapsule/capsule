---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes an allocated ResourcePoolClaim its namespace does not use"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: removes, from: Allocated }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Delete a claim that is not in use

## Trigger

A namespace no longer needs the claimed amounts.

## Outcome

The claim is gone and its amounts return to the pool.
