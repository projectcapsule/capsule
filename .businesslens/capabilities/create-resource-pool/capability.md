---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepool_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/resourcepool/pool_mutating.go" }
---

# Create ResourcePool

An administrator offers a budget of resources that selected namespaces can claim from.
