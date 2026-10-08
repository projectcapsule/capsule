---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits a pod that matches an audit rule"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: workload, effect: reads, facts: [Images] }
      - { entity: namespace, effect: reads, facts: [Effective rules] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product admits the pod and records an audit event for each audit rule it matches"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: workload, effect: creates, facts: [Kind, Images, Image pull policies, Priority class, Runtime class, Placement, Security profiles, Resources, Labels, Annotations] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Admit a workload an audit rule matches

## Trigger

A tenant owner deploys something an audit rule watches for.

## Outcome

The workload is admitted and an audit event records each audit rule it matched.
