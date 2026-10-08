---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a GlobalResourcePermitTemplate"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-resource-permit-template, effect: removes }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Withdraw a cluster-wide offer

## Trigger

The offer should no longer be requested anywhere.

## Outcome

The template is gone and new ResourcePermits for it are refused because the template is not found.

## Edge cases

- An approved permit for the template that has not yet started fails at activation, because its template can no longer be loaded.
