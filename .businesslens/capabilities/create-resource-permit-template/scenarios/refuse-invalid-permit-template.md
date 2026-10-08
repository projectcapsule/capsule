---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a ResourcePermitTemplate whose default duration exceeds its maximum duration"
    kind: actor
    actor: administrator
    entities:
      - { entity: resource-permit-template, effect: reads, facts: [Default duration, Maximum duration] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the ResourcePermitTemplate and states both durations"
    kind: condition
    entities:
      - { entity: resource-permit-template, effect: reads, facts: [Default duration, Maximum duration] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse an inconsistent template

## Trigger

An administrator gives a default duration longer than the maximum duration.

## Outcome

The template is refused.

## Edge cases

- An approver with an empty name, approval conditions that do not compile, or resources that cannot be parsed are refused.
