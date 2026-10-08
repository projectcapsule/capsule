---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner uncordons a TenantResource"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: tenant-resource, effect: changes, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product applies the replicated resources again and records the outcome of every item"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: replicated-resource, effect: creates, facts: [Origin, Protected, Deletion policy] }
      - { entity: tenant-resource, effect: changes, facts: [Processed items, Ready] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Resume a replication

## Trigger

A paused replication should keep its copies in sync again.

## Outcome

Capsule applies the objects the TenantResource describes again and removes those it no longer produces.
