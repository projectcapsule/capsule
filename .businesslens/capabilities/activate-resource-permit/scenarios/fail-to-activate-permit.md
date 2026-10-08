---
kind: edge
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
  - text: "The Product cannot apply a rendered resource and marks the permit failed at activation"
    kind: product
    entities:
      - { entity: resource-permit, effect: changes, from: Approved, to: Failed, facts: [Failure, Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Fail activation

## Trigger

Applying a rendered resource fails at the start time.

## Outcome

The permit is Failed at activation and can be retried; what was already applied can still be cleaned up.

## Edge cases

- A permit whose template can no longer be loaded, or whose resolved identity is missing, fails at activation in the same way.
