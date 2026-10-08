---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a LoadBalancer Service in a Tenant that forbids load balancers"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: service, effect: reads, facts: [Type] }
      - { entity: tenant, effect: reads, facts: [Service options] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the Service because its type is forbidden for the Tenant"
    kind: condition
    entities:
      - { entity: service, effect: reads, facts: [Type] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a Service type the tenant forbids

## Trigger

A tenant owner creates a LoadBalancer Service in a tenant that forbids them.

## Outcome

No Service is created.
