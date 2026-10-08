---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/rules/networkpolicies/validation/egress.go" }
  - { kind: code, role: implementation, target: "pkg/api/rules/enforce_networkpolicies_types.go" }
---

# Create NetworkPolicy

A tenant owner creates a NetworkPolicy in a tenant namespace, admitted only when its egress stays within the address ranges the namespace's effective rules allow.
