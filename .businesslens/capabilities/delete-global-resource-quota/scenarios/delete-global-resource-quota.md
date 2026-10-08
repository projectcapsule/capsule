---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a GlobalResourceQuota"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-resource-quota, effect: removes }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Remove a shared quota

## Trigger

The namespaces no longer need to share a limit.

## Outcome

The GlobalResourceQuota is gone, and so are the namespace quotas it kept in the namespaces it selected.
