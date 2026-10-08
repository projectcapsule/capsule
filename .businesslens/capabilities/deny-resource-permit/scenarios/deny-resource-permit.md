---
kind: primary
routes:
  api: Kubernetes API
  cli: Capsule CLI
steps:
  - text: "The Permit approver reviews a requested ResourcePermit and its rendered resources"
    kind: actor
    actor: permit-approver
    entities:
      - { entity: resource-permit, effect: reads, facts: [Duration, Keep for, Rendered resources] }
    contexts:
      api: { place: kubernetes-api::permit-review }
      cli: { place: capsule-cli::permit-review::permit-review }
  - text: "The Permit approver denies it with a message"
    kind: actor
    actor: permit-approver
    entities:
      - { entity: resource-permit, effect: changes, from: Requested, to: Denied, facts: [Review, Transitions] }
    contexts:
      api: { place: kubernetes-api::permit-review }
      cli: { place: capsule-cli::permit-review::permit-review }
---

# Deny a requested permit

## Trigger

A reviewer does not accept a request.

## Outcome

The permit is Denied with the reviewer's message and nothing is created.

## Edge cases

- An approved or active permit cannot be denied.
