---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator lowers the limit of a GlobalCustomQuota below its usage"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-custom-quota, effect: reads, facts: [Limit, Used] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the change and states the usage"
    kind: condition
    entities:
      - { entity: global-custom-quota, effect: reads, facts: [Used] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse lowering a global custom quota below usage

## Trigger

An administrator lowers the limit below current usage.

## Outcome

The GlobalCustomQuota keeps its limit.

## Edge cases

- Changing its sources while it records usage is refused.
