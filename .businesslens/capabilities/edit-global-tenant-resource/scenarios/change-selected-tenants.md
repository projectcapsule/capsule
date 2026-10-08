---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator changes the tenant selector of a GlobalTenantResource"
    kind: actor
    actor: administrator
    entities:
      - { entity: global-tenant-resource, effect: changes, facts: [Tenant selector] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Change the tenants a replication reaches

## Trigger

An administrator wants the objects in a different set of tenants.

## Outcome

Capsule then replicates as the GlobalTenantResource now says: tenants it now selects receive its objects; copies in tenants it no longer selects are removed or left in place as their deletion policy says.
