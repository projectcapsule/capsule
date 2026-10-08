---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/globalcustomquota_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/customquota/globalcustomquota_validating.go" }
---

# Create GlobalCustomQuota

An administrator limits a quantity counted or summed from objects of chosen kinds across selected namespaces.
