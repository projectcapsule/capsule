---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner labels a ServiceAccount in their Tenant for owner promotion"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: service-account, effect: changes, facts: [Owner promotion] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product makes the ServiceAccount an effective owner of the Tenant with the promotion cluster roles"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Effective owners] }
      - { entity: capsule-configuration, effect: reads, facts: [Promotion cluster roles] }
      - { entity: service-account, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Promote a ServiceAccount to tenant owner

## Trigger

Automation in the tenant needs to act as an owner.

## Outcome

The ServiceAccount is an effective owner of the Tenant with the promotion cluster roles.
