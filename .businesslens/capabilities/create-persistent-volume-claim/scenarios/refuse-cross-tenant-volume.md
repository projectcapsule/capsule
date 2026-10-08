---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a PersistentVolumeClaim naming a volume that belongs to another tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: persistent-volume-claim, effect: reads, facts: [Volume] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the claim to prevent a cross-tenant mount"
    kind: condition
    entities:
      - { entity: persistent-volume-claim, effect: reads, facts: [Volume] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a claim on another tenant's volume

## Trigger

A tenant owner names a volume that belongs to another tenant.

## Outcome

No claim is created.

## Edge cases

- A named volume without a tenant label is refused as well.
