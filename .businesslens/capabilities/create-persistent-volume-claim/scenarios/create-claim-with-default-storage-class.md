---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a PersistentVolumeClaim without a storage class"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: persistent-volume-claim, effect: reads, facts: [Storage class] }
      - { entity: tenant, effect: reads, facts: [Storage classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product sets the default storage class of the Tenant, limits the claim to volumes of the Tenant and admits it"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: persistent-volume-claim, effect: creates, facts: [Storage class, Volume selector] }
      - { entity: tenant, effect: reads, facts: [Storage classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Create a claim with the tenant's default storage class

## Trigger

A tenant owner needs storage.

## Outcome

The claim exists with the Tenant's default storage class and can bind only volumes of the Tenant.

## Edge cases

- Once bound, the volume is labelled with the claim's tenant.
