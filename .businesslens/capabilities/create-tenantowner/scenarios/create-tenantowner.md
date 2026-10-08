---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a TenantOwner with labels"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenantowner, effect: creates, facts: [Kind, Name, Cluster roles, Aggregate, Labels] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product makes it an effective owner of every Tenant whose owner selectors match it"
    kind: product
    actor: administrator
    entities:
      - { entity: tenantowner, effect: changes, facts: [Tenants] }
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

# Create a TenantOwner tenants select

## Trigger

The same identity should own several tenants.

## Outcome

The identity is an effective owner of every Tenant whose owner selectors match the TenantOwner, or that carries the tenant label pointing at it.

## Edge cases

- A TenantOwner that aggregates also counts its identity as a Capsule user.
