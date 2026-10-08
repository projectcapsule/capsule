---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "An approved ResourcePermit reaches its start time"
    kind: condition
    unattended: true
    entities:
      - { entity: resource-permit, effect: reads, facts: [Start time] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product creates the rendered resources as the resolved identity, marking them as granted by the permit"
    kind: product
    entities:
      - { entity: granted-resource, effect: creates, facts: [Active until, Protected, Deletion policy] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product makes the permit active until its duration ends"
    kind: product
    entities:
      - { entity: resource-permit, effect: changes, from: Approved, to: Active, facts: [Active until, Keep until, Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Activate a permit at its start time

## Trigger

An approved permit reaches its start time.

## Outcome

The granted resources exist and the permit is Active until its duration ends.

## Edge cases

- A permit approved without a start time, or whose start time has passed, is activated at once.
- Before activating, Capsule checks again that the reviewer was one of the approvers and that the approval conditions still hold.
