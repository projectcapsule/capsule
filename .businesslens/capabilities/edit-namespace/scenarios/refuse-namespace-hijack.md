---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner patches a namespace that belongs to a tenant they do not own"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Tenant] }
      - { entity: tenant, effect: reads, facts: [Effective owners] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the patch"
    kind: condition
    entities:
      - { entity: namespace, effect: reads, facts: [Tenant] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse changing another tenant's namespace

## Trigger

A tenant owner patches a namespace that belongs to a tenant they do not own.

## Outcome

The namespace is unchanged and a hijack attempt event is recorded.
