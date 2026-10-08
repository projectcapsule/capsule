---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "An active ResourcePermit reaches the end of its duration"
    kind: condition
    unattended: true
    entities:
      - { entity: resource-permit, effect: reads, facts: [Active until] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product expires it as the system"
    kind: product
    entities:
      - { entity: resource-permit, effect: changes, from: Active, to: Expired, facts: [Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Expire a permit when its duration ends

## Trigger

An active permit reaches the end of its duration.

## Outcome

The permit is Expired by the system.
