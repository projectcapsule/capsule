---
availability:
  - { place: kubernetes-api::tenant-workspace }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/serviceaccounts/promotion.go" }
  - { kind: code, role: implementation, target: "pkg/tenant/promotions.go#CollectPromotions" }
---

# Promote ServiceAccount

A tenant owner marks a ServiceAccount for promotion so namespace rules that promote matching ServiceAccounts grant it their cluster roles in the namespaces they select.
