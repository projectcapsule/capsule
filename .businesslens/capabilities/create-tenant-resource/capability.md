---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: replications
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_namespaced.go" }
  - { kind: code, role: implementation, target: "internal/webhook/defaults/replications.go" }
  - { kind: doc, role: context, target: "https://projectcapsule.dev/docs/replications/" }
---

# Create TenantResource

A tenant owner sets up replication of objects into namespaces of their own tenant.
