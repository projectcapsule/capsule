---
kind: alternative
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
  - text: "The Product renders the resources of the template, checks them with a dry run as the resolved identity and asks for review"
    kind: product
    entities:
      - { entity: resource-permit, effect: changes, from: Created, to: Requested, facts: [Rendered resources, Resolved identity, Transitions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Render a new permit and ask for review

## Trigger

A ResourcePermit has been created.

## Outcome

The permit is Requested and a review-needed event is recorded.

## Edge cases

- A template that cannot be rendered keeps the permit Created and not ready, with the partial result visible.
