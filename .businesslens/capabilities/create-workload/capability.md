---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/pod/handler.go" }
  - { kind: code, role: implementation, target: "internal/webhook/defaults/pods.go" }
  - { kind: code, role: implementation, target: "internal/webhook/rules/pods/validation/factory.go" }
  - { kind: code, role: implementation, target: "internal/webhook/rules/generic/mutation/workload_placement.go" }
  - { kind: code, role: implementation, target: "pkg/ruleengine/enforce_evaluator.go#EvaluateEnforce" }
  - { kind: code, role: implementation, target: "internal/webhook/globalresourcequota/calculation.go" }
---

# Create workload

A tenant owner creates a pod or pod controller in a tenant namespace. Capsule fills in the tenant's defaults and the mutations of the namespace's effective rules, then admits it only within the tenant's allowed classes and registries, the effective rules and the quotas covering the namespace.
