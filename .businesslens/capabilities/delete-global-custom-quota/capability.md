---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/webhook/customquota/globalcustomquota_validating.go" }
  - { kind: code, role: implementation, target: "internal/controllers/customquotas/global_custom_quota_controller.go" }
---

# Delete GlobalCustomQuota

An administrator removes a GlobalCustomQuota.
