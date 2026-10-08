---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/globalresourcequota_types.go" }
  - { kind: code, role: implementation, target: "internal/controllers/globalresourcequotas/controller.go" }
---

# Create GlobalResourceQuota

An administrator sets a resource quota shared by all namespaces it selects.
