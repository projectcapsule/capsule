---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a Tenant"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: changes, from: Active, to: Terminating, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product starts deleting every namespace of the Tenant and deletes the quotas its rules generated"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, from: Active, to: Terminating, facts: [] }
      - { entity: global-resource-quota, effect: removes }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product removes the Tenant once nothing of it remains"
    kind: product
    actor: administrator
    entities:
      - { entity: tenant, effect: removes, from: Terminating }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Delete a tenant and its namespaces

## Trigger

A team no longer needs its tenant.

## Outcome

The Tenant, its namespaces and the quotas its rules generated are gone.

## Edge cases

- A terminating Tenant refuses new namespaces.
- A namespace that keeps terminating after its pods are gone has its remaining content and foreign finalizers cleared.
