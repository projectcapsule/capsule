---
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
  - { kind: code, role: context, target: "charts/capsule/templates/rbac-tenants.yaml" }
---

# Kubernetes role

A role the cluster's own access control defines and binds to users, groups and ServiceAccounts, outside Capsule. Capsule leaves to these roles who may write Tenants, the CapsuleConfiguration, its cluster-wide resources and permit templates, and who may review or end ResourcePermits beyond what Capsule checks itself.

## Information kept

- **Name** — the role's name
- **Permissions** — the resources and operations it allows
- **Subjects** — the users, groups and ServiceAccounts it is bound to
