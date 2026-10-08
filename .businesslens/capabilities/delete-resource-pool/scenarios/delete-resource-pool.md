---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a ResourcePool"
    kind: actor
    actor: administrator
    entities:
      - { entity: resource-pool, effect: reads, facts: [Delete bound claims] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product removes the pool quota from every selected namespace and returns each ResourcePoolClaim to unassigned"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: resource-pool-claim, effect: changes, from: Allocated, to: Unassigned, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product removes the ResourcePool"
    kind: product
    actor: administrator
    entities:
      - { entity: resource-pool, effect: removes }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Withdraw a pool

## Trigger

The budget is no longer offered.

## Outcome

The pool and its namespace quotas are gone and its claims are unassigned.

## Edge cases

- With delete bound claims set, claims that were not released are deleted with the pool.
