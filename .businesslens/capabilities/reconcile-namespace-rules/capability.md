---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: rules
references:
  - { kind: code, role: implementation, target: "internal/controllers/tenant/rulestatus.go" }
  - { kind: code, role: implementation, target: "internal/controllers/rulestatus/manager.go" }
  - { kind: code, role: implementation, target: "pkg/tenant/rules.go#BuildNamespaceRuleBodyStatus" }
  - { kind: code, role: implementation, target: "pkg/tenant/rule_quota.go#RuleGlobalResourceQuota" }
---

# Reconcile namespace rules

Capsule composes the rules selecting each namespace into that namespace's effective rules, and carries out what they keep in place: managed metadata, permissions and quotas.
