---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a workload a CustomQuota counts while that quota has no room left"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [Labels] }
      - { entity: custom-quota, effect: reads, facts: [Limit, Used] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses it and states the requested, used and available amounts"
    kind: condition
    entities:
      - { entity: custom-quota, effect: reads, facts: [Limit] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse an object beyond a custom quota

## Trigger

A tenant owner creates an object a CustomQuota counts while that quota has no room left, in a cluster where the administrator routes that kind to custom quota admission.

## Outcome

The object is not created; the refusal states the requested, used and available amounts.

## Edge cases

- The smallest matching quota is checked first.
- Objects that leave a quota's count when deleted free their share once the deletion is observed.
