---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "A ResourcePermit is created"
    kind: condition
    unattended: true
    entities:
      - { entity: resource-permit, effect: reads, facts: [Template, Parameters] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product finds the resolved identity cannot apply the rendered resources and marks the permit failed"
    kind: product
    entities:
      - { entity: resource-permit, effect: changes, from: Created, to: Failed, facts: [Rendered resources, Resolved identity, Failure, Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Fail a request the identity cannot apply

## Trigger

The resolved identity is missing or cannot create the rendered resources.

## Outcome

The permit is Failed at preflight with the reason, and can be retried.
