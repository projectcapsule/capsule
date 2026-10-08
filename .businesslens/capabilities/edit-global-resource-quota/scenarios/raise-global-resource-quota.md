---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator raises the hard limits of a GlobalResourceQuota"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-resource-quota, effect: changes, facts: [Hard limits] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Raise a shared quota

## Trigger

The namespaces need more room.

## Outcome

The selected namespaces can use up to the new limits.
