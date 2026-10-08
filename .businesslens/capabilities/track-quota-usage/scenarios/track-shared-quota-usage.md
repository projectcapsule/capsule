---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "Usage changes in a namespace a shared quota selects, or a namespace starts or stops matching it"
    kind: condition
    unattended: true
    entities:
      - { entity: namespace, effect: reads, facts: [Labels] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
  - text: "The Product records usage per namespace and overall for each GlobalResourceQuota and GlobalCustomQuota"
    kind: product
    entities:
      - { entity: global-resource-quota, effect: changes, facts: [Used, Available, Namespace usage] }
      - { entity: global-custom-quota, effect: changes, facts: [Used, Available, Contributors] }
      - { entity: namespace, effect: reads, facts: [] }
    contexts:
      api: { place: kubernetes-api::cluster-administration }
---

# Keep shared quotas current across namespaces

## Trigger

Usage changes in a namespace a GlobalResourceQuota or GlobalCustomQuota selects, or a namespace starts or stops matching one.

## Outcome

Each shared quota reports usage per namespace and overall. In every namespace a GlobalResourceQuota selects, its namespace quota allows that namespace's own usage plus what remains overall, and a namespace that no longer matches loses that namespace quota.

## Edge cases

- When the shared quota is used up, each namespace's quota is held at what it already uses.
