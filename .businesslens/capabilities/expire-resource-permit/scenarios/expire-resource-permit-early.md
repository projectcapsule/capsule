---
kind: primary
routes:
  api: Kubernetes API
  cli: Capsule CLI
steps:
  - text: "The Tenant owner expires an active ResourcePermit"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-permit, effect: changes, from: Active, to: Expired, facts: [Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
      cli: { place: capsule-cli::tenant-workspace }
---

# End a permit early

## Trigger

The requester no longer needs the resources.

## Outcome

The permit is Expired; Capsule then removes its granted resources.
