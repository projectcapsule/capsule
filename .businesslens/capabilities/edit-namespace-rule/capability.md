---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: rules
references:
  - { kind: code, role: implementation, target: "internal/webhook/tenant/validation/rule_validator.go" }
  - { kind: code, role: implementation, target: "pkg/runtime/quota/validation.go" }
---

# Edit namespace rule

An administrator changes one of a Tenant's rules, including which namespaces it selects and its place in the order.
