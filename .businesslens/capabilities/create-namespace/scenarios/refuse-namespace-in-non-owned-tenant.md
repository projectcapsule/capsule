---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace labelled with a tenant they do not own"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Labels] }
      - { entity: tenant, effect: reads, facts: [Effective owners] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses to put the namespace in a Tenant the owner does not own"
    kind: condition
    entities:
      - { entity: namespace, effect: reads, facts: [Labels] }
      - { entity: tenant, effect: reads, facts: [Effective owners] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a namespace for a tenant not owned

## Trigger

A tenant owner labels a new namespace with a tenant they do not own.

## Outcome

No namespace is created.
