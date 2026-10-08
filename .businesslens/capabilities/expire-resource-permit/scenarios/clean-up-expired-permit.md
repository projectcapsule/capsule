---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "A ResourcePermit has expired"
    kind: condition
    unattended: true
    entities:
      - { entity: resource-permit, effect: reads, facts: [Keep until] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product removes the granted resources, leaving in place those whose deletion policy keeps them"
    kind: product
    entities:
      - { entity: granted-resource, effect: removes }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product deletes the ResourcePermit once its retention ends"
    kind: product
    entities:
      - { entity: resource-permit, effect: removes, from: Expired }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Remove an expired permit's resources and the permit

## Trigger

A permit has expired.

## Outcome

Its granted resources are removed, except those whose deletion policy keeps them, and the permit is deleted once its retention ends.
