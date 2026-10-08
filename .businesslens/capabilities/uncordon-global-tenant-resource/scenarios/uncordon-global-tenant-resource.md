---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator uncordons a GlobalTenantResource"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-tenant-resource, effect: changes, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product applies the replicated resources again and records the outcome of every item"
    kind: product
    actor: administrator
    entities:
      - { entity: replicated-resource, effect: creates, facts: [Origin, Protected, Deletion policy] }
      - { entity: global-tenant-resource, effect: changes, facts: [Processed items, Ready] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Resume a cluster-wide replication

## Trigger

A paused cluster-wide replication should keep its copies in sync again.

## Outcome

Capsule applies the objects the GlobalTenantResource describes in the selected tenants again and removes those it no longer produces.
