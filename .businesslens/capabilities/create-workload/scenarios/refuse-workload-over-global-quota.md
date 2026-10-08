---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a pod whose requests would exceed a GlobalResourceQuota covering the namespace"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [Resources] }
      - { entity: global-resource-quota, effect: reads, facts: [Hard limits, Used] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the pod and states the requested, current, projected and hard amounts"
    kind: condition
    entities:
      - { entity: global-resource-quota, effect: reads, facts: [Hard limits] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a workload beyond a shared quota

## Trigger

A tenant owner deploys a pod whose requests would exceed a GlobalResourceQuota covering the namespace.

## Outcome

No pod is created; the refusal states the requested, current, projected and hard amounts.

## Edge cases

- While a GlobalResourceQuota has not yet observed usage in all its namespaces, requests it covers cannot be admitted.
- When several quotas cover a request and one refuses, what the others reserved is released.
