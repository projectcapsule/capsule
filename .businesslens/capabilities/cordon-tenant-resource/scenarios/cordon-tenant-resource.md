---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner cordons a TenantResource"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: tenant-resource, effect: changes, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product leaves the replicated resources of the TenantResource as they are"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: tenant-resource, effect: reads, facts: [Cordoned] }
      - { entity: replicated-resource, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Pause a replication

## Trigger

A tenant owner needs to stop a replication from touching its copies for a while, for example during a migration.

## Outcome

The TenantResource reports that it is cordoned. Capsule applies and removes nothing for it, and the copies it already placed stay as they are.

## Edge cases

- Deleting a cordoned TenantResource still removes the copies it created.
