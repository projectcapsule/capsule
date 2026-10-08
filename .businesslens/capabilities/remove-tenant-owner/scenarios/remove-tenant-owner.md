---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator removes a user from the owners of a Tenant"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: changes, facts: [Owners] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product drops the user from the effective owners and removes its role bindings from every namespace of the Tenant"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Role bindings] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records the effective owners of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Effective owners] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Remove an owner from a tenant

## Trigger

Someone should no longer own the tenant.

## Outcome

The former owner no longer holds the Tenant's cluster roles in its namespaces and can no longer create namespaces in it.
