---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator adds a namespace rule to the rules of a Tenant"
    kind: actor
    actor: administrator
    entities:
      - { entity: namespace-rule, effect: creates, facts: [Position, Namespace selector, Audience, Action, Workload rules] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product publishes the rule in the effective rules of every namespace it selects"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Add a rule for some of a tenant's namespaces

## Trigger

Some namespaces of a tenant need a different profile, for example production namespaces that may only use one registry.

## Outcome

The namespaces the rule selects enforce it; the others are unaffected.
