---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/mutation/assignment.go" }
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
  - { kind: code, role: implementation, target: "internal/webhook/utils/tenant_get.go" }
  - { kind: code, role: implementation, target: "pkg/tenant/get_by.go" }
---

# Create namespace

A tenant owner creates a namespace and Capsule places it in one of the tenants they own, applying that tenant's policy.
