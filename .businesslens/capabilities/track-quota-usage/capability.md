---
availability:
  - { place: kubernetes-api::tenant-workspace }
  - { place: kubernetes-api::cluster-administration }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "internal/controllers/customquotas/calculation.go" }
  - { kind: code, role: implementation, target: "internal/controllers/globalresourcequotas/controller.go#projectedResourceQuotaSpec" }
---

# Track quota usage

Capsule keeps the usage of CustomQuotas, GlobalCustomQuotas and GlobalResourceQuotas current as the objects they count change, and keeps each selected namespace's quota in line with what remains overall.
