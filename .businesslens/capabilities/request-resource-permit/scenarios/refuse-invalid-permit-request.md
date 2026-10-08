---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner creates a ResourcePermit whose duration exceeds the maximum duration of the template"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-permit, effect: reads, facts: [Duration] }
      - { entity: resource-permit-template, effect: reads, facts: [Maximum duration] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the ResourcePermit and states both durations"
    kind: condition
    entities:
      - { entity: resource-permit, effect: reads, facts: [Duration] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a request outside the template's limits

## Trigger

A tenant owner asks for longer than the template allows.

## Outcome

No ResourcePermit is created.

## Edge cases

- Parameters that do not match the template's schema are refused.
- A global template not available in the namespace is refused.
- For a template that approves automatically, a request matching none of its conditions is refused.
