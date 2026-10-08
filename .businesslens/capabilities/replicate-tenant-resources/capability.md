---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: replications
references:
  - { kind: code, role: implementation, target: "internal/controllers/resources/namespaced.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resources/collect.go" }
  - { kind: code, role: implementation, target: "internal/controllers/resources/namespace_trigger.go" }
  - { kind: code, role: implementation, target: "pkg/template/validator_namespaces.go" }
---

# Replicate tenant resources

Capsule keeps the objects a TenantResource describes present and in sync in the selected namespaces of its tenant, acting as the TenantResource's ServiceAccount, and never outside that tenant.
