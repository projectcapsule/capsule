---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator removes a namespace rule from the rules of a Tenant"
    kind: actor
    actor: administrator
    entities:
      - { entity: namespace-rule, effect: removes }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product drops it from the effective rules of the namespaces it selected and deletes the quotas it generated"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Effective rules] }
      - { entity: global-resource-quota, effect: removes }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Remove a rule

## Trigger

A namespace profile no longer needs a rule.

## Outcome

The rule no longer applies anywhere and the quotas it generated are gone.
