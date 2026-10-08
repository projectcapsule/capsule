---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Administrator submits a Tenant whose allowed hostname pattern does not compile"
    kind: actor
    actor: administrator
    entities:
      - { entity: tenant, effect: reads, facts: [Ingress options] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product refuses the Tenant and names the invalid field"
    kind: condition
    entities:
      - { entity: tenant, effect: reads, facts: [Ingress options] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Refuse a tenant with an invalid field

## Trigger

An administrator submits a Tenant with a mistake in it.

## Outcome

No Tenant is created and the refusal names the invalid field.

## Edge cases

- A ServiceAccount owner not written as system:serviceaccount:<namespace>:<name> is refused.
- A ServiceAccount role binding subject whose name is not a valid DNS name is refused.
- An invalid namespace rule is refused.
