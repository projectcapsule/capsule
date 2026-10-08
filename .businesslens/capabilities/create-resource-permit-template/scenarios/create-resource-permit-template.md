---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a ResourcePermitTemplate in a namespace"
    kind: actor
    actor: administrator
    entities:
      - { entity: resource-permit-template, effect: creates, facts: [Impersonation, Resources, Parameter schema, Context, Default duration, Maximum duration, Keep for, Auto approval, Approvers, Approval conditions] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Offer resources on request in a namespace

## Trigger

The platform team wants to grant temporary access or resources on request.

## Outcome

The template is ready and permits can be requested for it in its namespace.
