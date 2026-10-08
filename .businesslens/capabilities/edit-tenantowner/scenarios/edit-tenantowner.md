---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator changes the labels and cluster roles of a TenantOwner"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenantowner, effect: changes, facts: [Cluster roles, Labels] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product recomputes the Tenants the TenantOwner is an effective owner of"
    kind: product
    actor: administrator
    entities:
      - { entity: tenantowner, effect: changes, facts: [Tenants] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product binds the new cluster roles in the namespaces of those Tenants"
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

# Change a TenantOwner

## Trigger

The identity should hold other roles, or be owner of a different set of tenants.

## Outcome

The identity is an effective owner of exactly the Tenants whose owner selectors now match it, with the new cluster roles bound in their namespaces.
