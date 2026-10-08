---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes a CustomQuota"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: custom-quota, effect: removes }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Remove a custom quota

## Trigger

A namespace no longer needs the limit.

## Outcome

The CustomQuota is gone and the objects it counted are no longer limited by it.
