---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a Gateway with a class the Tenant does not allow"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: gateway, effect: reads, facts: [Gateway class] }
      - { entity: tenant, effect: reads, facts: [Gateway classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the Gateway"
    kind: condition
    entities:
      - { entity: gateway, effect: reads, facts: [Gateway class] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a gateway class the tenant does not allow

## Trigger

A tenant owner creates a Gateway with a class outside the tenant's allowed list.

## Outcome

No Gateway is created.
