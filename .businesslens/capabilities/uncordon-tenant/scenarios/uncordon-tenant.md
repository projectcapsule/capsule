---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator uncordons a cordoned Tenant"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: changes, from: Cordoned, to: Active, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product removes the cordoned marker from every namespace of the Tenant"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Managed metadata] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Uncordon a tenant

## Trigger

The reason for the freeze has passed.

## Outcome

The Tenant is Active again and its owners work in it as before. An event records the change.
