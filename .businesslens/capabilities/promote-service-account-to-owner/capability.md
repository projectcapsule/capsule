---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/serviceaccounts/owner_promotion.go" }
  - { kind: code, role: implementation, target: "pkg/tenant/owners.go#CollectOwners" }
---

# Promote ServiceAccount to owner

A tenant owner makes a ServiceAccount in their tenant an owner of the tenant, for example for automation that creates namespaces.
