---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator cordons a Tenant"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: changes, from: Active, to: Cordoned, facts: [Cordoned] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product marks every namespace of the Tenant as cordoned"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Managed metadata] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Cordon a tenant

## Trigger

The administrator needs to freeze a tenant, for example during an incident or a migration.

## Outcome

The Tenant is Cordoned. Its owners can no longer create, change or delete namespaces or the resources in them; running workloads keep running. An event records the change.
