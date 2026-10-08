---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator changes the namespace selectors of a GlobalResourcePermitTemplate"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-resource-permit-template, effect: changes, facts: [Namespace selectors] }
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

# Change where a cluster-wide offer applies

## Trigger

The offer should be available in a different set of namespaces.

## Outcome

The template resolves the namespaces it can now be requested in; new permits in other namespaces are refused. Permits already rendered keep what they captured.

## Edge cases

- A default duration longer than the maximum duration, an approver with an empty name, approval conditions that do not compile or resources that cannot be parsed are refused, as when it is created.
- Until the changed selection is resolved, requests for the template are refused as not ready.
