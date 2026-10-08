---
appliesTo:
  - { type: entity, id: tenantowner, effect: creates }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows create TenantOwners

Capsule validates what is written but does not check who creates TenantOwners; the cluster's Kubernetes roles decide.
