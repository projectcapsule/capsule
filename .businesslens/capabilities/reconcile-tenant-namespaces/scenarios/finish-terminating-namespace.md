---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "A namespace of a terminating Tenant has had no pods for the grace period"
    kind: condition
    unattended: true
    entities:
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product deletes the remaining content of the namespace and clears foreign finalizers"
    kind: product
    entities:
      - { entity: namespace, effect: removes, from: Terminating }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Finish deleting a stuck namespace of a deleted tenant

## Trigger

A namespace of a terminating Tenant still exists after its pods are gone.

## Outcome

The namespace finishes deleting.
