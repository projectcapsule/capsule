---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a GlobalCustomQuota"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-custom-quota, effect: removes }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Remove a global custom quota

## Trigger

The namespaces no longer need the shared limit.

## Outcome

The GlobalCustomQuota is gone and the objects it counted are no longer limited by it.
