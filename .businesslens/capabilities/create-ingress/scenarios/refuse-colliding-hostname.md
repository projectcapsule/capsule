---
kind: edge
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits an Ingress whose hostname and path another Ingress in the collision scope already uses"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: ingress, as: new-ingress, effect: reads, facts: [Hostnames] }
      - { entity: ingress, as: existing-ingress, effect: reads, facts: [Hostnames] }
      - { entity: tenant, effect: reads, facts: [Ingress options] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the Ingress because the hostname is already used"
    kind: condition
    entities:
      - { entity: ingress, as: existing-ingress, effect: reads, facts: [Hostnames] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a hostname already in use

## Trigger

A tenant owner publishes a hostname and path another Ingress already serves within the collision scope.

## Outcome

The new Ingress is not created.

## Edge cases

- The collision scope is the whole set of tenant namespaces, the tenant, the namespace, or disabled.
