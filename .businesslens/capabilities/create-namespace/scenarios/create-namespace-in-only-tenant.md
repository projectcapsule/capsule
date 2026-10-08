---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace without naming a tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product picks the only Tenant the owner owns"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: tenant, effect: reads, facts: [Effective owners] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product creates the namespace in that Tenant with its managed metadata"
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

# Create a namespace in the only tenant owned

## Trigger

A tenant owner who owns one tenant needs a new namespace.

## Outcome

The namespace belongs to the Tenant and carries its managed metadata; the owners' roles are bound in it.

## Edge cases

- A name matching the protected namespace pattern is refused for everyone.
- A namespace missing metadata the Tenant requires, or carrying metadata it forbids, is refused.
- Namespace rules that target namespaces can default, require or refuse its labels and annotations.
- A Tenant that is terminating refuses new namespaces.
