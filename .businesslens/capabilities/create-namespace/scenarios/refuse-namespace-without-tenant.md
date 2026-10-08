---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace with no tenant label and a name matching none of their tenants"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name, Labels] }
      - { entity: tenant, effect: reads, facts: [Name] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the namespace and asks for the tenant label"
    kind: condition
    entities:
      - { entity: namespace, effect: reads, facts: [Labels] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a namespace no tenant can be chosen for

## Trigger

A tenant owner who owns several tenants gives neither a tenant label nor a matching name.

## Outcome

No namespace is created; the refusal asks for the tenant label.

## Edge cases

- An identity that owns no tenant is told to ask the administrators for one.
