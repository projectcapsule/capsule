---
kind: validation
routes:
  api: Kubernetes API
  cli: Capsule CLI
steps:
  - text: "The Permit approver approves a ResourcePermit whose request matches none of the approval conditions"
    kind: actor
    actor: permit-approver
    entities:
      - { entity: resource-permit, effect: reads, facts: [Rendered resources] }
    contexts:
      api: { place: kubernetes-api::permit-review }
      cli: { place: capsule-cli::permit-review::permit-review }
  - text: "The Product refuses the approval because the approval conditions are not satisfied"
    kind: condition
    entities:
      - { entity: resource-permit, effect: reads, facts: [Review] }
      - { entity: resource-permit-template, effect: reads, facts: [Approval conditions] }
    contexts:
      api: { place: kubernetes-api::permit-review }
      cli: { place: capsule-cli::permit-review::permit-review }
---

# Refuse an approval the template does not permit

## Trigger

A permit approver approves a permit whose request does not satisfy the template's approval conditions.

## Outcome

The permit stays Requested.

## Edge cases

- A permit whose resources are not ready cannot be approved.
- An approval that would make the duration exceed the template maximum is refused.
- A denied permit cannot be approved.
