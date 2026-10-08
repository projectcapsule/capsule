---
appliesTo:
  - { type: entity, id: global-tenant-resource, effect: removes }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows delete GlobalTenantResources

Capsule validates what is written but does not check who deletes GlobalTenantResources; the cluster's Kubernetes roles decide.
