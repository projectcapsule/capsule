---
appliesTo:
  - { type: entity, id: tenant, effect: creates }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows create Tenants

Capsule validates what is written but does not check who creates Tenants; the cluster's Kubernetes roles decide.
