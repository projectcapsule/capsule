---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a GlobalResourceQuota selecting namespaces by label"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-resource-quota, effect: creates, facts: [Namespace selectors, Hard limits] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product gives each selected namespace a quota of what it uses plus what remains overall and records usage per namespace"
    kind: product
    actor: administrator
    entities:
      - { entity: global-resource-quota, effect: changes, facts: [Used, Available, Namespace usage] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Share a quota across namespaces

## Trigger

Several namespaces should share one limit.

## Outcome

Together the selected namespaces cannot exceed the hard limits; usage is reported per namespace.
