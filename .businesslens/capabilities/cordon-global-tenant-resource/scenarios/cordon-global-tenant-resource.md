---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator cordons a GlobalTenantResource"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-tenant-resource, effect: changes, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product leaves the replicated resources of the GlobalTenantResource as they are"
    kind: product
    actor: administrator
    entities:
      - { entity: global-tenant-resource, effect: reads, facts: [Cordoned] }
      - { entity: replicated-resource, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Pause a cluster-wide replication

## Trigger

An administrator needs to stop a GlobalTenantResource from touching its copies for a while.

## Outcome

The GlobalTenantResource reports that it is cordoned. Capsule applies and removes nothing for it in any tenant, and existing copies stay as they are.

## Edge cases

- Deleting a cordoned GlobalTenantResource still removes the copies it created.
