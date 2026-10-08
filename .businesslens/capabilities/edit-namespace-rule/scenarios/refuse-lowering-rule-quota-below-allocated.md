---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator lowers a rule quota below what is already allocated"
    kind: actor
    actor: administrator
    entities:
      - { entity: namespace-rule, effect: reads, facts: [Quotas] }
      - { entity: global-resource-quota, effect: reads, facts: [Used] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the change and states what is allocated"
    kind: condition
    entities:
      - { entity: namespace-rule, effect: reads, facts: [Quotas] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse lowering a rule quota below allocation

## Trigger

An administrator lowers a rule quota below what its namespaces already use or have reserved.

## Outcome

The rule keeps its quota.

## Edge cases

- Lowering a rule quota while also changing the rule's selector is refused until usage is reconciled.
