---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner creates a CustomQuota summing a field across objects of a kind in their namespace"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: custom-quota, effect: creates, facts: [Limit, Sources, Scope selectors] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product counts the matching objects and records usage and contributors"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: custom-quota, effect: changes, facts: [Used, Available, Contributors] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Limit a custom quantity in a namespace

## Trigger

A tenant owner wants to cap something no built-in quota covers.

## Outcome

The CustomQuota reports usage, what is available and which objects contribute.
