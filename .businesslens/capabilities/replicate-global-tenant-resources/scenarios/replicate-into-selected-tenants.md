---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "A GlobalTenantResource changes, its resync period passes, or a Tenant starts or stops matching its tenant selector"
    kind: condition
    unattended: true
    entities:
      - { entity: global-tenant-resource, effect: reads, facts: [Tenant selector] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product renders its items once per selected namespace, once per Tenant or once for the cluster, as its scope says"
    kind: product
    entities:
      - { entity: global-tenant-resource, effect: reads, facts: [Scope, Resources] }
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product applies each replicated resource and removes those it no longer produces"
    kind: product
    entities:
      - { entity: replicated-resource, as: copy, effect: creates, facts: [Origin, Protected, Deletion policy] }
      - { entity: replicated-resource, as: stale-copy, effect: removes }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records the selected tenants and the outcome of every item"
    kind: product
    entities:
      - { entity: global-tenant-resource, effect: changes, facts: [Selected tenants, Processed items, Ready] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Replicate into selected tenants

## Trigger

A GlobalTenantResource changes, its resync period passes, or a tenant starts or stops matching it.

## Outcome

The selected tenants hold the replicated objects, and the GlobalTenantResource reports the tenants it selected and each item's outcome.

## Edge cases

- Tenants being deleted are skipped.
