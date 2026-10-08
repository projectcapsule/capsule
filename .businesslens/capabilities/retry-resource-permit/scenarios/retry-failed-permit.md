---
kind: primary
routes:
  api: Kubernetes API
  cli: Capsule CLI
steps:
  - text: "The Tenant owner asks for a failed ResourcePermit to be retried"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-permit, effect: changes, from: Failed, to: Retrying, facts: [Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
      cli: { place: capsule-cli::tenant-workspace }
  - text: "The Product repeats the failed preflight and returns the permit to requested"
    kind: product
    entities:
      - { entity: resource-permit, effect: changes, from: Retrying, to: Requested, facts: [Failure, Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
      cli: { place: capsule-cli::tenant-workspace }
---

# Retry a failed permit

## Trigger

The cause of a failure, such as missing permissions, has been fixed.

## Outcome

The permit returns to Requested after a preflight failure, or to Approved and activates after an activation failure.
