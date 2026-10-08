---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "A ResourcePermit is requested for a template that approves automatically and the request matches its approval conditions"
    kind: condition
    unattended: true
    entities:
      - { entity: resource-permit, effect: reads, facts: [] }
      - { entity: resource-permit-template, effect: reads, facts: [Auto approval, Approval conditions] }
    contexts:
      api: { place: kubernetes-api::permit-review }
  - text: "The Product approves it as the system"
    kind: product
    entities:
      - { entity: resource-permit, effect: changes, from: Requested, to: Approved, facts: [Review, Transitions] }
    contexts:
      api: { place: kubernetes-api::permit-review }
---

# Approve a permit automatically

## Trigger

A permit is requested for a template that approves automatically, and the request matches its conditions.

## Outcome

The permit is Approved by the system with the message Auto Approved.
