---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes a ResourcePoolClaim in use"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: reads, facts: [Pool] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the deletion while the claim is in use"
    kind: condition
    entities:
      - { entity: resource-pool-claim, effect: reads, facts: [Pool] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse deleting a claim in use

## Trigger

A tenant owner deletes a claim current usage needs.

## Outcome

The claim remains.
