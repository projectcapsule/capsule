---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a new namespace for a Tenant that already holds its namespace quota"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Namespace quota, Namespaces] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the namespace because the Tenant has reached its namespace quota"
    kind: condition
    entities:
      - { entity: namespace, effect: reads, facts: [Name] }
      - { entity: tenant, effect: reads, facts: [Namespace quota] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a namespace beyond the namespace quota

## Trigger

A tenant owner creates a namespace in a tenant that already holds its namespace quota.

## Outcome

No namespace is created; an overprovisioning event is recorded on the namespace, linked to its Tenant.
