---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes a namespace of a cordoned Tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the deletion because the Tenant is cordoned"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse deleting a namespace of a cordoned tenant

## Trigger

A tenant owner deletes a namespace while the tenant is cordoned.

## Outcome

The namespace is untouched.
