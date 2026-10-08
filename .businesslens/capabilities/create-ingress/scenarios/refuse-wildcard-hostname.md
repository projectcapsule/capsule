---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits an Ingress with a wildcard hostname in a Tenant that does not allow wildcards"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: ingress, effect: reads, facts: [Hostnames] }
      - { entity: tenant, effect: reads, facts: [Ingress options] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the wildcard for the Tenant"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Ingress options] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse a wildcard hostname

## Trigger

A tenant owner publishes a wildcard hostname in a tenant that does not allow wildcards.

## Outcome

No Ingress is created.
