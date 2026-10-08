---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The rules or data of a Tenant, or the labels of one of its namespaces, change"
    kind: condition
    unattended: true
    entities:
      - { entity: tenant, effect: reads, facts: [Data] }
      - { entity: namespace, effect: reads, facts: [Labels] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product selects the rules for each namespace, keeps their order and renders their templates for it"
    kind: product
    entities:
      - { entity: namespace-rule, effect: reads, facts: [Position, Namespace selector] }
      - { entity: namespace, effect: changes, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product applies managed metadata from the effective rules to the namespace and to existing objects of the targeted kinds"
    kind: product
    entities:
      - { entity: namespace, effect: changes, facts: [Labels, Annotations] }
      - { entity: workload, effect: changes, facts: [Labels, Annotations] }
      - { entity: service, effect: changes, facts: [Labels, Annotations] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Publish each namespace's effective rules

## Trigger

A Tenant's rules or data, or a namespace's labels, change.

## Outcome

Each namespace's effective rules list, in order, the rules that select it, rendered for it; admission uses them from then on.

## Edge cases

- A rule whose template cannot be rendered leaves the namespace's effective rules not ready, and the previously published rules stay in force.
- Rules gated by conditions apply their managed and default metadata only to matching requests.
