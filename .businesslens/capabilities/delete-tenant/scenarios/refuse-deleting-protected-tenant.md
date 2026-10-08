---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator deletes a Tenant that prevents deletion"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: reads, facts: [Prevent deletion] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the deletion because the Tenant is protected"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Prevent deletion] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse deleting a protected tenant

## Trigger

Someone deletes a Tenant that prevents deletion.

## Outcome

The Tenant and its namespaces are untouched.
