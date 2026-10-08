---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/gateway/validate_class.go" }
  - { kind: code, role: implementation, target: "internal/webhook/defaults/gateway.go" }
---

# Create Gateway

A tenant owner creates a Gateway in a tenant namespace, admitted only with a gateway class the tenant allows.
