---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner changes a label of a Node whose key the CapsuleConfiguration forbids"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: node, effect: reads, facts: [Protected labels] }
      - { entity: capsule-configuration, effect: reads, facts: [Forbidden node labels] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the change and lists the forbidden labels"
    kind: condition
    entities:
      - { entity: node, effect: reads, facts: [Protected labels] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse changing a forbidden node label

## Trigger

A tenant owner changes a node label whose key the CapsuleConfiguration forbids.

## Outcome

The Node is unchanged; the refusal lists the forbidden labels.

## Edge cases

- Forbidden annotations are protected in the same way.
