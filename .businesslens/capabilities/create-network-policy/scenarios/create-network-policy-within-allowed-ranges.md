---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a NetworkPolicy whose egress stays within the address ranges the effective rules allow"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: network-policy, effect: reads, facts: [Egress destinations] }
      - { entity: namespace, effect: reads, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product admits the NetworkPolicy"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: network-policy, effect: creates, facts: [Egress destinations] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Create a NetworkPolicy within the allowed ranges

## Trigger

A tenant owner opens egress for an application.

## Outcome

The NetworkPolicy exists.
