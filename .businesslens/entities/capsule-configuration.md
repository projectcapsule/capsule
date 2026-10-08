---
singleton: true
references:
  - { kind: code, role: implementation, target: "api/v1beta2/capsuleconfiguration_types.go" }
  - { kind: code, role: implementation, target: "pkg/runtime/configuration/client.go#DefaultCapsuleConfiguration" }
---

# CapsuleConfiguration

The cluster-wide settings that decide who Capsule treats as tenant users and administrators and how namespaces, promotion and impersonation behave. Capsule creates it with defaults when it is missing.

## Information kept

- **Users** — users, groups and ServiceAccounts treated as Capsule users
- **Ignored user groups** — groups whose members Capsule leaves alone, even when they are administrators
- **Administrators** — users, groups and ServiceAccounts treated as administrators
- **Force tenant prefix** — whether namespace names must start with their tenant name and a dash, unless a Tenant overrides it
- **Protected namespace pattern** — a regular expression no tenant namespace name may match
- **Service account promotion** — whether ServiceAccounts may be promoted at all
- **Administration cluster roles** — cluster roles administrators receive in every tenant namespace
- **Promotion cluster roles** — cluster roles a ServiceAccount promoted to owner receives
- **Forbidden node labels** — node label keys Capsule users may not add, change or remove
- **Forbidden node annotations** — node annotation keys Capsule users may not add, change or remove
- **Default service accounts** — the identities replications and permit templates act as when they name none
- **Cache invalidation** — how often Capsule rebuilds its compiled caches
- **Capsule users** — the effective set of Capsule users, including TenantOwners counted as users
- **Tenants** — the names of all tenants
