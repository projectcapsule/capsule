---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner changes a label of a Node whose key the CapsuleConfiguration does not forbid"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: node, effect: changes, facts: [Labels] }
      - { entity: capsule-configuration, effect: reads, facts: [Forbidden node labels] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Change an unprotected node label

## Trigger

A tenant owner labels a node of their dedicated pool.

## Outcome

The Node carries the new label.
