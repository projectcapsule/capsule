---
appliesTo:
  - { type: capability, id: create-workload }
  - { type: capability, id: create-network-policy }
  - { type: entity, id: namespace-rule, facts: [Action] }
references:
  - { kind: code, role: implementation, target: "pkg/ruleengine/enforce_evaluator.go#EvaluateEnforce" }
---

# An audit rule never refuses a request

A matching audit rule records an audit event on the tenant and never changes whether the request is admitted.
