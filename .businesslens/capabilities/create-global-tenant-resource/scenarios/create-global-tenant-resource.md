---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a GlobalTenantResource selecting tenants by label"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-tenant-resource, effect: creates, facts: [Tenant selector, Scope, Resources, Resync period, Service account, Cordoned, Depends on] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Replicate objects into selected tenants

## Trigger

The platform team wants the same objects in every namespace of a set of tenants.

## Outcome

The GlobalTenantResource exists and Capsule starts replicating its objects.
