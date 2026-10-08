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
  - text: "The Permit approver approves it, optionally changing its duration, start time or retention"
    kind: actor
    actor: permit-approver
    entities:
      - { entity: resource-permit, effect: changes, from: Requested, to: Approved, facts: [Review, Duration, Start time, Keep for, Transitions] }
    contexts:
      api: { place: kubernetes-api::permit-review }
      cli: { place: capsule-cli::permit-review::permit-review }
---

# Approve a requested permit

## Trigger

A permit is waiting for review.

## Outcome

The permit is Approved and its resources are created at its start time; the transition records the reviewer.
