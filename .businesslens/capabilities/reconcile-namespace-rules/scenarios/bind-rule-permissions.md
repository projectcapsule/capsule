---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "A namespace rule grants permissions"
    kind: condition
    unattended: true
    entities:
      - { entity: namespace-rule, effect: reads, facts: [Permissions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product binds the rule's cluster roles to its subjects in every namespace it selects"
    kind: product
    entities:
      - { entity: namespace, effect: changes, facts: [Role bindings] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Bind the roles rules grant

## Trigger

A rule grants cluster roles to subjects.

## Outcome

The subjects hold those roles in every namespace the rule selects and in no other namespace.
