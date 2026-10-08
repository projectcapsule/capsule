---
appliesTo:
  - { type: capability, id: reconcile-tenant-namespaces }
  - { type: capability, id: reconcile-namespace-rules }
  - { type: entity, id: namespace, facts: [Role bindings] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/managed.go" }
  - { kind: code, role: context, target: "charts/capsule/values.yaml" }
---

# Objects Capsule manages are written only by Capsule and administrators

Objects labelled as managed by Capsule, such as the role bindings and quotas it keeps in tenant namespaces and the GlobalResourceQuotas namespace rules declare, are created, changed or deleted only by Capsule or an administrator; everyone else is refused. While a namespace is terminating, its managed objects can be deleted so the namespace can finish.
