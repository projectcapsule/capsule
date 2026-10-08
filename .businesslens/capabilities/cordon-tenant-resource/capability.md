---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: replications
references:
  - { kind: code, role: implementation, target: "internal/controllers/resources/namespaced.go" }
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_types.go" }
---

# Cordon TenantResource

A tenant owner pauses a replication: Capsule stops applying and removing its objects until it is uncordoned.
