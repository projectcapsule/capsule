---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner creates a CustomQuota with a limit of zero"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: custom-quota, effect: reads, facts: [Limit] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the CustomQuota"
    kind: condition
    entities:
      - { entity: custom-quota, effect: reads, facts: [Limit] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse an invalid custom quota

## Trigger

A tenant owner gives a limit of zero or an expression that does not compile.

## Outcome

No CustomQuota is created.
