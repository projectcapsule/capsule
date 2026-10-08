---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes a TenantResource"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: tenant-resource, effect: removes }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Stop replicating a tenant's objects

## Trigger

A tenant owner no longer wants the objects replicated.

## Outcome

The TenantResource is deleted once Capsule has removed or released the objects it replicated.
