---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a PersistentVolumeClaim with a storage class the Tenant does not allow"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: persistent-volume-claim, effect: reads, facts: [Storage class] }
      - { entity: tenant, effect: reads, facts: [Storage classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the claim and lists the allowed storage classes"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Storage classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a storage class the tenant does not allow

## Trigger

A tenant owner asks for a storage class outside the tenant's allowed list.

## Outcome

No claim is created; the refusal lists the allowed storage classes.
