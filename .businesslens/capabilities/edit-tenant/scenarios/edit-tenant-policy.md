---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator lowers the quota and changes the allowed storage classes of a Tenant"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: changes, facts: [Namespace quota, Storage classes] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product applies the changed policy to every namespace of the Tenant"
    kind: product
    actor: administrator
    entities:
      - { entity: namespace, effect: changes, facts: [Managed metadata] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Change a tenant's policy

## Trigger

The administrator adjusts what a tenant may use.

## Outcome

Every namespace of the Tenant follows the changed policy; requests made afterwards are checked against it.
