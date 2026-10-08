---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a pod in a namespace of a cordoned Tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [] }
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the request because the namespace is cordoned"
    kind: condition
    entities:
      - { entity: namespace, effect: reads, facts: [Managed metadata] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a workload in a cordoned tenant

## Trigger

A tenant owner deploys into a namespace of a cordoned tenant.

## Outcome

Nothing is created; the refusal says the namespace is cordoned.
