---
appliesTo:
  - { type: capability, id: reconcile-namespace-rules }
  - { type: entity, id: global-resource-quota, facts: [Namespace selectors] }
references:
  - { kind: code, role: implementation, target: "pkg/tenant/rule_quota.go#RuleGlobalResourceQuota" }
---

# A rule quota only ever covers namespaces of its own tenant

The quota a rule declares covers the namespaces the rule selects within its tenant, never another tenant's namespaces.
