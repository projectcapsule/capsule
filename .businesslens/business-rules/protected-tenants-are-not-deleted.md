---
appliesTo:
  - { type: entity, id: tenant, effect: changes, to: Terminating }
permits:
  - configuredBy: kubernetes-role
    when:
      - { fact: Prevent deletion, is: false }
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
  - { kind: code, role: implementation, target: "internal/webhook/tenant/validation/protected.go" }
---

# Only identities a Kubernetes role allows delete a Tenant, and never while it prevents deletion

Deleting a Tenant that prevents deletion is refused for everyone. Otherwise Capsule leaves who may delete a Tenant to the cluster's Kubernetes roles, which give tenant owners no such access by default.
