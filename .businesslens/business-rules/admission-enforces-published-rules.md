---
appliesTo:
  - { type: capability, id: reconcile-namespace-rules }
  - { type: capability, id: create-workload }
  - { type: entity, id: namespace, facts: [Effective rules] }
references:
  - { kind: code, role: implementation, target: "pkg/runtime/handlers/typed_tenant_ruleset.go" }
---

# Admission enforces the effective rules published for each namespace

Requests in a namespace are checked against the effective rules last published for it. Until a namespace's rules are first published, they are worked out from its tenant for each request; namespaces themselves are always checked against their tenant's current rules.
