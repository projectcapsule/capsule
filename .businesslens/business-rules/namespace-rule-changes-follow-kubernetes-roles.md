---
appliesTo:
  - { type: entity, id: namespace-rule, effect: changes }
permits:
  - configuredBy: kubernetes-role
  - unattended: true
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows change a Tenant's namespace rules

Namespace rules are part of their Tenant, so whoever a Kubernetes role lets write the Tenant may change them; Capsule validates the rules but does not check who writes them.
