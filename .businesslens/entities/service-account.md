---
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/serviceaccounts/owner_promotion.go" }
  - { kind: code, role: implementation, target: "internal/webhook/serviceaccounts/promotion.go" }
---

# ServiceAccount

A workload identity in a tenant namespace that a tenant owner can promote, either to owner of the tenant or to the cluster roles a namespace rule grants.

## Information kept

- **Name** — the ServiceAccount name
- **Owner promotion** — whether it is promoted to owner of its tenant
- **Promotion** — whether it is promoted to the cluster roles of the namespace rules that select it
