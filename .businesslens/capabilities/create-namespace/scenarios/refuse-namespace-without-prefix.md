---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace for a Tenant that forces its prefix, without the prefix"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Name, Force tenant prefix] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the namespace and names the prefix the Tenant expects"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Name, Force tenant prefix] }
      - { entity: namespace, effect: reads, facts: [Name] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a namespace without the forced prefix

## Trigger

A tenant owner creates a namespace in a tenant that forces the name prefix.

## Outcome

No namespace is created; the refusal names the expected prefix.
