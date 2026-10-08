---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner changes the claimed amounts of a ResourcePoolClaim its namespace does not use"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: changes, facts: [Claimed resources] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Resize a claim that is not in use

## Trigger

A namespace needs a different amount.

## Outcome

The claim is allocated again with the new amounts if they fit.
