---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/webhook/customquota/customquota_validating.go" }
  - { kind: code, role: implementation, target: "internal/controllers/customquotas/custom_quota_controller.go" }
---

# Delete CustomQuota

A tenant owner removes a CustomQuota from their namespace.
