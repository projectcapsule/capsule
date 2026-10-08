---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner labels a ServiceAccount for promotion while the CapsuleConfiguration disables it"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: service-account, effect: reads, facts: [Name] }
      - { entity: capsule-configuration, effect: reads, facts: [Service account promotion] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the label because promotion is disabled"
    kind: condition
    entities:
      - { entity: service-account, effect: reads, facts: [Promotion] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse promotion while promotion is disabled

## Trigger

A tenant owner promotes a ServiceAccount while the CapsuleConfiguration disables promotion.

## Outcome

The ServiceAccount is not promoted.
