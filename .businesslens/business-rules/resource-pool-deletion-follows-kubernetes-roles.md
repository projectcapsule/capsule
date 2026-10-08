---
appliesTo:
  - { type: entity, id: resource-pool, effect: removes }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows delete ResourcePools

Capsule validates what is written but does not check who deletes ResourcePools; the cluster's Kubernetes roles decide.
