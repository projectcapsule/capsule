---
appliesTo:
  - { type: entity, id: resource-permit-template, effect: removes }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows delete ResourcePermitTemplates

Capsule validates what is written but does not check who deletes ResourcePermitTemplates; the cluster's Kubernetes roles decide.
