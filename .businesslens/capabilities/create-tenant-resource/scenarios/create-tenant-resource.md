---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner creates a TenantResource in a namespace of their Tenant listing objects to copy and namespaces to copy them to"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: tenant-resource, effect: creates, facts: [Resources, Resync period, Service account, Cordoned, Depends on] }
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Replicate objects across a tenant's namespaces

## Trigger

A tenant owner wants the same objects, such as a pull secret or a config map, in several of their namespaces.

## Outcome

The TenantResource exists and Capsule starts replicating its objects.

## Edge cases

- Deprecated adoption and pruning settings are converted into per-item lifecycle policies.
- A lifecycle condition expression that does not compile is refused.
