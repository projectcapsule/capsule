---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator submits a Tenant with its owners and policy"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: creates, to: Active, facts: [Name, Owners, Owner selectors, Owner promotion, Namespace quota, Force tenant prefix, Cordoned, Prevent deletion, Node selector, Storage classes, Ingress options] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records the effective owners and available classes of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Effective owners, Available classes] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Create a tenant with owners and policy

## Trigger

A team needs its own space on the cluster.

## Outcome

The Tenant is Active and its owners can create namespaces in it.

## Edge cases

- Deprecated policy fields are accepted with a warning naming what replaces them.
