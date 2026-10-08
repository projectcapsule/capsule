---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a pod in a namespace of their Tenant"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [Images] }
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product fills in the default priority and runtime classes of the Tenant and the mutations of the effective rules of the namespace"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: tenant, effect: reads, facts: [Priority classes, Runtime classes] }
      - { entity: namespace, effect: reads, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product checks the pod against the allowed classes and registries and the effective rules and admits it"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: workload, effect: creates, facts: [Kind, Images, Image pull policies, Priority class, Runtime class, Placement, Security profiles, Resources, Labels, Annotations] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Create a workload the tenant allows

## Trigger

A tenant owner deploys an application.

## Outcome

The pod runs with the Tenant's default classes and the mutations its namespace's rules apply.

## Edge cases

- Mutations apply only to new pods and to ephemeral containers newly added to a pod; existing containers are never changed.
- A mutation merges with what the pod sets unless it says to replace each property it supplies; the mutated pod is still checked against the rules.
- Pod controllers are checked against workload rules only when a rule targets their kind.
- Ephemeral containers added later are checked for registry, profile, scheduler and QoS rules.
- Resource defaults and limit ratios from workload rules are written into new pods and targeted controllers.
