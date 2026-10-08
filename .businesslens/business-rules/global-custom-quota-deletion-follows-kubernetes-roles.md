---
appliesTo:
  - { type: entity, id: global-custom-quota, effect: removes }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows delete GlobalCustomQuotas

Capsule validates what is written but does not check who deletes GlobalCustomQuotas; the cluster's Kubernetes roles decide.
