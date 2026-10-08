---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a GlobalResourcePermitTemplate selecting namespaces by label"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-resource-permit-template, effect: creates, facts: [Namespace selectors, Impersonation, Resources, Parameter schema, Context, Default duration, Maximum duration, Keep for, Auto approval, Approvers, Approval conditions] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product resolves the namespaces it can be requested in"
    kind: product
    actor: administrator
    entities:
      - { entity: global-resource-permit-template, effect: changes, facts: [Available namespaces] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Offer resources on request across namespaces

## Trigger

The platform team wants the same offer available in many namespaces.

## Outcome

The template resolves the namespaces it can be requested in.

## Edge cases

- When a namespace's labels change, the namespaces the template resolves to are recomputed.
