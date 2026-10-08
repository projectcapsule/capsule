---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/customquota_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/customquota/customquota_validating.go" }
---

# Create CustomQuota

A tenant owner limits a quantity counted or summed from objects of chosen kinds in their namespace.
