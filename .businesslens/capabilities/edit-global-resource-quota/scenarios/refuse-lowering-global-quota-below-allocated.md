---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator lowers a hard limit of a GlobalResourceQuota below what is allocated"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-resource-quota, effect: reads, facts: [Hard limits, Used] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the change and states what is allocated"
    kind: condition
    entities:
      - { entity: global-resource-quota, effect: reads, facts: [Used] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse lowering a shared quota below allocation

## Trigger

An administrator lowers a limit below what is already used or reserved.

## Outcome

The quota keeps its limits.

## Edge cases

- Lowering a limit while also changing the selectors is refused until usage is reconciled.
