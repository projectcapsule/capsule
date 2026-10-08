---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator raises the quota of a ResourcePool"
    kind: actor
    actor: administrator
    entities:
      - { entity: resource-pool, effect: changes, facts: [Quota] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product allocates exhausted claims that now fit"
    kind: product
    actor: administrator
    entities:
      - { entity: resource-pool-claim, effect: changes, from: Exhausted, to: Allocated, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Grow a pool

## Trigger

Claims are waiting because the pool ran out.

## Outcome

Waiting claims that now fit are allocated.

## Edge cases

- A namespace that stops matching the pool loses its pool quota and its claims are returned to unassigned.
