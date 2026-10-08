---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace for a cordoned Tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the namespace because the Tenant is cordoned"
    kind: condition
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a namespace in a cordoned tenant

## Trigger

A tenant owner creates a namespace in a cordoned tenant.

## Outcome

No namespace is created.
