---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner changes the claimed amounts of a ResourcePoolClaim in use"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-pool-claim, effect: reads, facts: [Claimed resources] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the change while the claim is allocated and in use"
    kind: condition
    entities:
      - { entity: resource-pool-claim, effect: reads, facts: [Pool] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse changing a claim in use

## Trigger

A tenant owner changes a claim that current usage needs.

## Outcome

The claim is unchanged.
