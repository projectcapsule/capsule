---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator creates a GlobalCustomQuota counting objects of a kind across namespaces selected by label"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-custom-quota, effect: creates, facts: [Limit, Namespace selectors, Sources, Scope selectors] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product counts the matching objects and records usage and contributors"
    kind: product
    actor: administrator
    entities:
      - { entity: global-custom-quota, effect: changes, facts: [Used, Available, Contributors] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Limit a custom quantity across namespaces

## Trigger

The platform team wants to cap something across many namespaces.

## Outcome

The GlobalCustomQuota reports the namespaces it covers, usage and contributors.
