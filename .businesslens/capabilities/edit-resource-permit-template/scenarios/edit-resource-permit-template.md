---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator changes the maximum duration and approvers of a ResourcePermitTemplate"
    kind: actor
    actor: administrator
    entities:
      - { entity: resource-permit-template, effect: changes, facts: [Maximum duration, Approvers] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Change a namespace offer

## Trigger

The platform team changes the resources, limits or approvers of an offer.

## Outcome

Permits requested afterwards use the changed template. A permit already rendered keeps the resources and approval rules captured when it was rendered.

## Edge cases

- A default duration longer than the maximum duration, an approver with an empty name, approval conditions that do not compile or resources that cannot be parsed are refused, as when it is created.
