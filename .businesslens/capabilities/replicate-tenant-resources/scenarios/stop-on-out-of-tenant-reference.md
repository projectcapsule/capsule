---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "A TenantResource refers to a namespace outside its Tenant"
    kind: condition
    unattended: true
    entities:
      - { entity: tenant-resource, effect: reads, facts: [Resources] }
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product applies and removes nothing and reports the TenantResource not ready with the refused reference"
    kind: product
    entities:
      - { entity: tenant-resource, effect: changes, facts: [Ready] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Stop on a reference outside the tenant

## Trigger

A TenantResource refers to a namespace outside its tenant.

## Outcome

Nothing is applied or removed and existing copies stay; the TenantResource is not ready and names the refused reference.
