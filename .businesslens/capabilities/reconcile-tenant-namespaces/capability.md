---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/controllers/tenant/manager.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenant/namespaces.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenant/rolebindings.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenant/resourcequotas.go" }
---

# Reconcile tenant namespaces

Capsule keeps every namespace of a tenant in line with the tenant: its effective owners and their role bindings, managed metadata, and the quotas, limit ranges and network policies the tenant distributes.
