---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner raises the limit of a CustomQuota"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: custom-quota, effect: changes, facts: [Limit] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Raise a custom quota

## Trigger

The namespace needs more of the counted quantity.

## Outcome

Objects up to the new limit are admitted.
