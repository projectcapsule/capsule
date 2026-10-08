---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace whose name starts with the name of one of their tenants and a dash"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Name] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product creates the namespace in the Tenant whose name is the longest matching prefix"
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

# Create a namespace in the tenant its name starts with

## Trigger

A tenant owner who owns several tenants names the namespace after one of them.

## Outcome

The namespace belongs to the Tenant with the longest name that prefixes it.
