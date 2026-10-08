---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner labels a ServiceAccount for owner promotion in a Tenant that does not allow it"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: service-account, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Owner promotion] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the label"
    kind: condition
    entities:
      - { entity: service-account, effect: reads, facts: [Owner promotion] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse owner promotion where it is disabled

## Trigger

A tenant owner promotes a ServiceAccount in a tenant that does not allow owner promotion.

## Outcome

The ServiceAccount is not promoted.

## Edge cases

- With promotion disabled in the CapsuleConfiguration, every promotion is refused.
