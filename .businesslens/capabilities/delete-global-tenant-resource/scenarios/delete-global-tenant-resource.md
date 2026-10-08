---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a GlobalTenantResource"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-tenant-resource, effect: removes }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Stop replicating into selected tenants

## Trigger

The objects are no longer needed in those tenants.

## Outcome

The GlobalTenantResource is deleted once Capsule has removed or released the objects it replicated.
