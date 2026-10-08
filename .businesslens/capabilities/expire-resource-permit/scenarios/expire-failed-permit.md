---
kind: alternative
routes:
  api: Kubernetes API
  cli: Capsule CLI
steps:
  - text: "The Tenant owner expires a failed ResourcePermit instead of retrying it"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-permit, effect: changes, from: Failed, to: Expired, facts: [Keep until, Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
      cli: { place: capsule-cli::tenant-workspace }
---

# End a permit that failed

## Trigger

A permit failed and the requester no longer wants it retried.

## Outcome

The permit is Expired; Capsule removes or releases whatever it had already applied, and keeps the permit for audit until its retention ends.

## Edge cases

- A permit can be expired from any phase, including one still waiting for review or approved but not yet started.
