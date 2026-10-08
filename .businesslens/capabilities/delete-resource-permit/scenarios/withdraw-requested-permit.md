---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner deletes a ResourcePermit waiting for review"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-permit, effect: removes, from: Requested }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Withdraw a permit before review

## Trigger

The requester no longer needs a permit that is still waiting for review.

## Outcome

The permit is gone.
