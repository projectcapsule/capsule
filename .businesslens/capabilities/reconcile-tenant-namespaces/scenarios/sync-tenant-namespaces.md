---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "A Tenant or one of its namespaces changes"
    kind: condition
    unattended: true
    entities:
      - { entity: tenant, effect: reads, facts: [] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product recomputes the effective owners and promotions of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Effective owners, Promotions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product applies managed metadata, role bindings and the distributed quotas, limit ranges and network policies of the Tenant to each namespace"
    kind: product
    entities:
      - { entity: namespace, effect: changes, facts: [Managed metadata, Role bindings] }
      - { entity: tenant, effect: reads, facts: [Namespace metadata, Node selector, Additional role bindings, Resource quotas, Limit ranges, Network policies, Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product records the namespaces and available classes of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Namespaces, Available classes] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Keep tenant namespaces in line with the tenant

## Trigger

A Tenant, one of its namespaces, a matched TenantOwner or a promoted ServiceAccount changes.

## Outcome

Every namespace carries the Tenant's managed metadata and role bindings, and the Tenant reports its effective owners, namespaces and state.

## Edge cases

- Managed keys the Tenant no longer sets are removed from its namespaces.
- With a tenant-wide resource quota reached, each namespace's quota is held at what it already uses.
- A namespace's labels and annotations are replaced outright when the Tenant manages its metadata only.
