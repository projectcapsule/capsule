---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a ResourcePool offering resources to namespaces selected by label"
    kind: actor
    actor: administrator
    entities:
      - { entity: resource-pool, effect: creates, facts: [Namespace selectors, Quota, Defaults, Defaults zero, Ordered queue, Delete bound claims] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product gives every selected namespace a quota holding the pool defaults"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: resource-pool, effect: changes, facts: [Allocation] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Offer a pool of resources to selected namespaces

## Trigger

The platform team wants namespaces to draw from a shared budget.

## Outcome

Every selected namespace has a quota holding the pool defaults and can claim from the pool.

## Edge cases

- With defaults zero, every offered resource defaults to zero in each namespace.
