---
appliesTo:
  - { type: entity, id: global-custom-quota, effect: changes }
permits:
  - configuredBy: kubernetes-role
  - unattended: true
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows change GlobalCustomQuotas

Capsule validates what is written but does not check who changes GlobalCustomQuotas; the cluster's Kubernetes roles decide. Capsule itself keeps their status current.
