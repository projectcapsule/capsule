---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner creates a ResourcePermit for a template with parameters, a reason and a duration"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: resource-permit, effect: creates, to: Created, facts: [Template, Parameters, Requestor, Reason, Duration, Start time] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Request resources for a limited time

## Trigger

A tenant owner needs something the template offers, for example temporary elevated access.

## Outcome

The permit is Created; Capsule renders it and asks for review.

## Edge cases

- The start time, when given, must be in the future.
