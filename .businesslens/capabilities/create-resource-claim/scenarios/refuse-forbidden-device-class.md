---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a ResourceClaim requesting a device class the Tenant does not allow"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-claim, effect: reads, facts: [Device classes] }
      - { entity: tenant, effect: reads, facts: [Device classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the ResourceClaim"
    kind: condition
    entities:
      - { entity: resource-claim, effect: reads, facts: [Device classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a device class the tenant does not allow

## Trigger

A tenant owner requests a device class outside the tenant's allowed list, or one that does not exist.

## Outcome

No ResourceClaim is created.
