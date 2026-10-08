---
appliesTo:
  - { type: entity, id: tenantowner, effect: removes }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows delete TenantOwners

Capsule validates what is written but does not check who deletes TenantOwners; the cluster's Kubernetes roles decide.
