---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: rules
references:
  - { kind: code, role: implementation, target: "pkg/api/rules/rule_body_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/tenant/validation/rule_validator.go" }
  - { kind: doc, role: context, target: "https://projectcapsule.dev/docs/tenants/rules/" }
---

# Add namespace rule

An administrator adds a rule to a Tenant's ordered rules, giving the namespaces it selects enforcement, mutation, permissions or quotas.
