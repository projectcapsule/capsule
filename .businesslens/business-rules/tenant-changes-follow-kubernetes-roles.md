---
appliesTo:
  - { type: entity, id: tenant, effect: changes, facts: [Owners, Owner selectors, Owner promotion, Namespace quota, Force tenant prefix, Namespace metadata, Managed metadata only, Node selector, Additional role bindings, Cordoned, Prevent deletion, Storage classes, Ingress options, Gateway classes, Priority classes, Runtime classes, Device classes, Service options, Container registries, Image pull policies, Pod metadata, Resource quotas, Limit ranges, Network policies, Data] }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
  - { kind: code, role: implementation, target: "internal/webhook/tenant/validation/handler.go" }
---

# Only identities a Kubernetes role allows change a Tenant's owners and policy

Capsule checks what a Tenant says, never who writes it; the cluster's Kubernetes roles decide. The roles Capsule itself binds give tenant owners no access to Tenants, so tenant owners never change the Tenant they own unless the cluster grants them that.
