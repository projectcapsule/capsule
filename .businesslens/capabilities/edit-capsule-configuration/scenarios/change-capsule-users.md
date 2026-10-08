---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator changes the Capsule users and administrators in the CapsuleConfiguration"
    kind: actor
    actor: administrator
    entities:
      - { entity: capsule-configuration, effect: changes, facts: [Users, Administrators] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product recomputes the effective Capsule users and the effective owners of every Tenant"
    kind: product
    actor: administrator
    entities:
      - { entity: capsule-configuration, effect: changes, facts: [Capsule users] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records the effective owners of the Tenant"
    kind: product
    entities:
      - { entity: tenant, effect: changes, facts: [Effective owners] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Change who Capsule treats as users and administrators

## Trigger

The platform team changes which groups use tenants or administer Capsule.

## Outcome

Capsule treats the new identities as Capsule users and administrators; administrators are effective owners of every Tenant.

## Edge cases

- Using the deprecated user name and group lists is accepted with a warning.
