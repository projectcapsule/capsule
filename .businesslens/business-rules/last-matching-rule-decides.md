---
appliesTo:
  - { type: capability, id: create-workload }
  - { type: capability, id: create-service }
  - { type: capability, id: create-ingress }
  - { type: capability, id: create-network-policy }
  - { type: entity, id: namespace-rule, facts: [Position, Action] }
references:
  - { kind: code, role: implementation, target: "pkg/ruleengine/enforce_evaluator.go#EvaluateEnforce" }
---

# The last matching allow or deny rule decides each enforced value

Rules are evaluated in their order. For each value a request carries, the last matching allow or deny rule decides; when allow rules exist for that property and none matches, the request is refused.
