---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator moves a namespace from one Tenant to another"
    kind: actor
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Tenant] }
      - { entity: tenant, as: source-tenant, effect: reads, facts: [] }
      - { entity: tenant, as: target-tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records the namespaces of the source Tenant"
    kind: product
    entities:
      - { entity: tenant, as: source-tenant, effect: changes, facts: [Namespaces] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records the namespaces of the target Tenant"
    kind: product
    entities:
      - { entity: tenant, as: target-tenant, effect: changes, facts: [Namespaces] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product replaces the managed metadata and role bindings of the namespace with those of the target Tenant"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Managed metadata, Role bindings] }
      - { entity: tenant, as: target-tenant, effect: reads, facts: [Namespace metadata, Owners] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Move a namespace to another tenant

## Trigger

A namespace should change hands between teams.

## Outcome

The namespace belongs to the target Tenant and follows its policy instead of the source Tenant's.
