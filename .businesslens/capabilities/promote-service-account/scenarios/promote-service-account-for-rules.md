---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner labels a ServiceAccount for promotion"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: service-account, effect: changes, facts: [Promotion] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product binds the cluster roles of each namespace rule whose promotion selects the ServiceAccount in the namespaces that rule selects"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: namespace-rule, effect: reads, facts: [Permissions, Namespace selector] }
      - { entity: namespace, effect: changes, facts: [Role bindings] }
      - { entity: service-account, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product records the promotions of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Promotions] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Promote a ServiceAccount through namespace rules

## Trigger

A workload identity needs roles across several namespaces of the tenant.

## Outcome

The ServiceAccount holds the promoted cluster roles in its own namespace and in every namespace the promoting rules select.
