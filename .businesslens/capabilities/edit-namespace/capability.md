---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/user_metadata.go" }
  - { kind: code, role: implementation, target: "pkg/tenant/owned.go#NamespaceIsOwned" }
---

# Edit namespace

A tenant owner changes the labels and annotations of a namespace in their tenant.
