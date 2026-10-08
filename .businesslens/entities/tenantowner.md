---
domain: tenants
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantowner_types.go" }
  - { kind: code, role: implementation, target: "internal/controllers/tenantowner/manager.go" }
---

# TenantOwner

An owner identity defined once, outside any Tenant, that tenants pull in as an owner through their owner selectors.

## Information kept

- **Kind** — User, Group or ServiceAccount
- **Name** — the identity name
- **Cluster roles** — cluster roles the owner receives in every namespace of the tenants it owns
- **Aggregate** — whether the identity also counts as a Capsule user
- **Labels** — labels tenants select it by
- **Tenants** — the tenants it is an effective owner of
