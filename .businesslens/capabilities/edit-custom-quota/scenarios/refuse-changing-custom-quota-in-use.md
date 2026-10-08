---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner changes the sources of a CustomQuota that records usage"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: custom-quota, effect: reads, facts: [Sources, Used] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the change and states the recorded usage"
    kind: condition
    entities:
      - { entity: custom-quota, effect: reads, facts: [Used] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse changing what a custom quota counts while it counts something

## Trigger

A tenant owner changes the sources of a CustomQuota that already records usage.

## Outcome

The CustomQuota is unchanged; the refusal suggests creating a new one.

## Edge cases

- Lowering the limit below current usage is refused.
