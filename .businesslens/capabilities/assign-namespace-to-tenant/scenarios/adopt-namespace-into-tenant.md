---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator labels an unassigned namespace with a Tenant and makes the Tenant its owner"
    kind: actor
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Tenant] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records the namespaces of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Namespaces] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product applies the managed metadata and role bindings of the Tenant to the namespace"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Managed metadata, Role bindings] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Adopt an existing namespace into a tenant

## Trigger

A namespace created outside Capsule should belong to a tenant.

## Outcome

The namespace belongs to the Tenant and follows its policy.

## Edge cases

- The tenant label and the owner reference must be set together.
