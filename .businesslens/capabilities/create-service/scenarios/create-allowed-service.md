---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a ClusterIP Service in a namespace of their Tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: service, effect: reads, facts: [Type] }
      - { entity: namespace, effect: reads, facts: [Effective rules] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product checks the Service against the Service options of the Tenant and the effective rules and admits it with the metadata the Tenant adds"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: service, effect: creates, facts: [Type, Labels, Annotations] }
      - { entity: tenant, effect: reads, facts: [Service options] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Create a Service the tenant allows

## Trigger

A tenant owner exposes an application inside the cluster.

## Outcome

The Service exists with the metadata the Tenant adds to Services.

## Edge cases

- External IPs outside the allowed ranges are refused; with no ranges allowed, any external IP is refused.
- Node ports outside the ranges service rules allow, or left to automatic allocation where rules require them, are refused.
- Labels and annotations the Tenant forbids on Services are refused.
