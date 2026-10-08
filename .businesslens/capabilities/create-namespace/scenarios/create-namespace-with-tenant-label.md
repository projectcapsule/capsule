---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace labelled with one of the tenants they own"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name, Labels] }
      - { entity: tenant, effect: reads, facts: [Name] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product creates the namespace in the labelled Tenant with its managed metadata"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: creates, to: Active, facts: [Name, Tenant, Labels, Annotations, Managed metadata] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product records the namespaces of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Namespaces] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Create a namespace in a labelled tenant

## Trigger

A tenant owner who owns several tenants names the one the namespace is for.

## Outcome

The namespace belongs to the labelled Tenant.
