---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/webhook/globalresourcequota/calculation.go" }
  - { kind: code, role: implementation, target: "pkg/runtime/quota/validation.go" }
---

# Edit GlobalResourceQuota

An administrator changes a shared quota's limits or selection.
