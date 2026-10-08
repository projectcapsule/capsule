---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a pod with a priority class the Tenant does not allow"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [Priority class] }
      - { entity: tenant, effect: reads, facts: [Priority classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the pod and lists the allowed priority classes"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Priority classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a class the tenant does not allow

## Trigger

A tenant owner deploys a pod with a priority class outside the tenant's allowed list.

## Outcome

No pod is created; the refusal lists the allowed priority classes.

## Edge cases

- Runtime classes, container registries and image pull policies set on the Tenant refuse in the same way.
