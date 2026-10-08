---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner removes the placement annotation the Tenant enforces from a namespace"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Managed metadata] }
      - { entity: tenant, effect: reads, facts: [Node selector] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the change because the Tenant enforces that placement"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Node selector] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse removing the enforced node selector

## Trigger

A tenant owner edits the node selector annotation the Tenant enforces on a namespace.

## Outcome

The namespace keeps the Tenant's node selector.
