---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a NetworkPolicy granting egress to an address range a deny rule covers"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: network-policy, effect: reads, facts: [Egress destinations] }
      - { entity: namespace, effect: reads, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the NetworkPolicy, names the range and records a warning event on the Tenant"
    kind: condition
    entities:
      - { entity: network-policy, effect: reads, facts: [Egress destinations] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse egress to a denied range

## Trigger

A tenant owner grants egress to an address range a rule denies.

## Outcome

No NetworkPolicy is created; the refusal names the range and a warning event is recorded on the Tenant.

## Edge cases

- An egress rule without destinations grants every address and is checked as such.
- With allow rules present, egress must fall entirely within the allowed ranges.
