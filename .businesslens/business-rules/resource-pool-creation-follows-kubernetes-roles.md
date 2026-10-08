---
appliesTo:
  - { type: entity, id: resource-pool, effect: creates }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows create ResourcePools

Capsule validates what is written but does not check who creates ResourcePools; the cluster's Kubernetes roles decide.
