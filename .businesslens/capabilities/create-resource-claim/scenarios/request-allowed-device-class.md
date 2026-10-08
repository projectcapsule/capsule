---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a ResourceClaim requesting a device class the Tenant allows"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-claim, effect: reads, facts: [Device classes] }
      - { entity: tenant, effect: reads, facts: [Device classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product admits the ResourceClaim"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: resource-claim, effect: creates, facts: [Device classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Request an allowed device class

## Trigger

A tenant owner needs devices such as GPUs.

## Outcome

The ResourceClaim exists.
