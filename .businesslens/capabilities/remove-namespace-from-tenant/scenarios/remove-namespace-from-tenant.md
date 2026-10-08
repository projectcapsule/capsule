---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator removes the tenant label and owner reference from a namespace"
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
---

# Take a namespace out of its tenant

## Trigger

A namespace should no longer belong to a tenant.

## Outcome

The namespace remains, no longer counted in the Tenant.
