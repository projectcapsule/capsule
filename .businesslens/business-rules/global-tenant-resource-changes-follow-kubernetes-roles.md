---
appliesTo:
  - { type: entity, id: global-tenant-resource, effect: changes }
permits:
  - configuredBy: kubernetes-role
  - unattended: true
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows change GlobalTenantResources

Capsule validates what is written but does not check who changes GlobalTenantResources; the cluster's Kubernetes roles decide. Capsule itself keeps their status current.
