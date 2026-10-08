---
kind: alternative
routes:
  api: Kubernetes API
steps:
  - text: "A namespace rule declares a named quota"
    kind: condition
    unattended: true
    entities:
      - { entity: namespace-rule, effect: reads, facts: [Quotas] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product keeps one GlobalResourceQuota per named quota covering only the namespaces of that Tenant the rule selects"
    kind: product
    entities:
      - { entity: global-resource-quota, effect: creates, facts: [Namespace selectors, Hard limits] }
      - { entity: namespace, effect: reads, facts: [] }
      - { entity: tenant, effect: reads, facts: [Name] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Keep the quotas rules declare

## Trigger

A rule declares a named quota.

## Outcome

One GlobalResourceQuota per named quota limits the Tenant's namespaces the rule selects, and no others.
