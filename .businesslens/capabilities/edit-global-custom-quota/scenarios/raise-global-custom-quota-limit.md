---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator raises the limit of a GlobalCustomQuota"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-custom-quota, effect: changes, facts: [Limit] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Raise a global custom quota

## Trigger

The namespaces need more of the counted quantity.

## Outcome

Objects up to the new limit are admitted.
