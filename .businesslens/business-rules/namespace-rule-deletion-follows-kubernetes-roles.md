---
appliesTo:
  - { type: entity, id: namespace-rule, effect: removes }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows delete a Tenant's namespace rules

Namespace rules are part of their Tenant, so whoever a Kubernetes role lets write the Tenant may delete them; Capsule validates the rules but does not check who writes them.
