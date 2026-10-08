---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "pkg/api/rbac/owner.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenant/rolebindings.go" }
---

# Add tenant owner

An administrator adds a user, group or ServiceAccount to a Tenant's owners.
