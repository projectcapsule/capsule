---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a Deployment without a label the effective rules require"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [Labels] }
      - { entity: namespace, effect: reads, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the Deployment and names the required label"
    kind: condition
    entities:
      - { entity: workload, effect: reads, facts: [Labels] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a workload without a required label

## Trigger

A tenant owner deploys a workload without a label the namespace's metadata rules require.

## Outcome

No workload is created; the refusal names the missing label.
