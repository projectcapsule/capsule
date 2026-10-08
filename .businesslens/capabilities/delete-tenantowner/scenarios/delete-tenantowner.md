---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a TenantOwner"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenantowner, effect: removes }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product removes it from the effective owners of the Tenants that selected it and removes its role bindings"
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

# Delete a TenantOwner

## Trigger

The identity should no longer own the tenants that selected it.

## Outcome

The identity is no longer an effective owner of those tenants unless a Tenant lists it directly.
