---
kind: validation
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a pod whose image comes from a registry the effective rules do not allow"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [Images] }
      - { entity: namespace, effect: reads, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product refuses the pod, names the image and the allowed registries, and records a warning event on the Tenant"
    kind: condition
    entities:
      - { entity: workload, effect: reads, facts: [Images] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Refuse an image from a registry the rules do not allow

## Trigger

A tenant owner deploys an image from a registry the namespace's profile does not allow.

## Outcome

No pod is created; the refusal names the image and the allowed registries, and a warning event is recorded on the Tenant.

## Edge cases

- An allowed image with a pull policy the registry rule does not allow is refused.
- A later allow rule for the image overrides an earlier deny rule.
- Placement, security profile, QoS class and workload kind rules refuse in the same way.
