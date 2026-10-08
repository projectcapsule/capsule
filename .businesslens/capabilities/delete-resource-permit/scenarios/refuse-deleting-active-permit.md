---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes an active ResourcePermit"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-permit, effect: reads, facts: [Active until] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the deletion until the permit has expired"
    kind: condition
    entities:
      - { entity: resource-permit, effect: reads, facts: [Active until] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse deleting a permit that has not expired

## Trigger

A tenant owner deletes an approved, active, denied or failed permit.

## Outcome

The permit remains; it has to expire first.

## Edge cases

- An expired permit cannot be deleted before its retention ends.
