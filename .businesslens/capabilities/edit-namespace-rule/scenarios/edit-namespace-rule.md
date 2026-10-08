---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator changes the selector and action of a namespace rule"
    kind: actor
    actor: administrator
    entities:
      - { entity: namespace-rule, effect: changes, facts: [Namespace selector, Action] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product republishes the effective rules of the namespaces it now selects and of those it no longer selects"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Change what a rule selects and does

## Trigger

A namespace profile changes.

## Outcome

Namespaces the rule now selects enforce it; namespaces it no longer selects stop enforcing it.
