---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner changes the labels of a namespace in their Tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: changes, facts: [Labels] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product keeps the managed metadata of the Tenant on the namespace"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: namespace, effect: reads, facts: [Managed metadata] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Change a namespace's labels

## Trigger

A tenant owner labels a namespace, for example so a namespace rule selects it.

## Outcome

The namespace carries the new labels together with the metadata its Tenant manages.

## Edge cases

- Adding or changing a label or annotation the Tenant forbids is refused; unchanged keys are not re-checked.
- Removing metadata the Tenant requires is refused.
- Moving the namespace into, out of or between tenants is refused.
- Tenant ownership cannot change while the namespace is terminating.
