---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a Gateway without a class"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: gateway, effect: reads, facts: [Listener hostnames] }
      - { entity: tenant, effect: reads, facts: [Gateway classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product sets the default gateway class of the Tenant and admits the Gateway"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: gateway, effect: creates, facts: [Gateway class, Listener hostnames] }
      - { entity: tenant, effect: reads, facts: [Gateway classes] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Create a Gateway with the tenant's default class

## Trigger

A tenant owner needs a Gateway.

## Outcome

The Gateway exists with the Tenant's default gateway class.
