---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "An object a CustomQuota counts is created, changed or deleted"
    kind: condition
    unattended: true
    entities:
      - { entity: custom-quota, effect: reads, facts: [Sources, Scope selectors] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product recounts the matching objects and records usage and contributors"
    kind: product
    entities:
      - { entity: custom-quota, effect: changes, facts: [Used, Available, Contributors] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Keep a custom quota's usage current

## Trigger

An object a CustomQuota counts is created, changed or deleted in its namespace.

## Outcome

The CustomQuota reports current usage, what is available and which objects contribute.

## Edge cases

- Subtracting sources never take usage below zero.
- An object counted by several quotas counts towards each of them.
