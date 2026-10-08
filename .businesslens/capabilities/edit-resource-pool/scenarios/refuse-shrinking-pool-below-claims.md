---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator lowers the quota of a ResourcePool below what claims hold"
    kind: actor
    actor: administrator
    entities:
      - { entity: resource-pool, effect: reads, facts: [Quota, Allocation] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the change and states the claimed amount"
    kind: condition
    entities:
      - { entity: resource-pool, effect: reads, facts: [Allocation] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse shrinking a pool below what is claimed

## Trigger

An administrator lowers or removes a resource that claims still hold.

## Outcome

The pool keeps its quota; the refusal asks to remove the claims first.
