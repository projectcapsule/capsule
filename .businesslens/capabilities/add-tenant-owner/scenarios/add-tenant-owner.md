---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator adds a group to the owners of a Tenant with the cluster roles it receives"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: changes, facts: [Owners] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product adds the group to the effective owners of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Effective owners] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product binds its cluster roles in every namespace of the Tenant"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Role bindings] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Add an owner to a tenant

## Trigger

Another person or group should own the tenant.

## Outcome

The new owner can create namespaces in the Tenant and holds its cluster roles in every namespace of it.
