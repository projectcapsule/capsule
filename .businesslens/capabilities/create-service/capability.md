---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/service/validating.go" }
  - { kind: code, role: implementation, target: "internal/webhook/rules/services/validation/service_type.go" }
  - { kind: code, role: implementation, target: "internal/controllers/servicelabels/service.go" }
---

# Create Service

A tenant owner creates a Service in a tenant namespace, admitted only with the types, addresses, ports and metadata the tenant and the namespace's effective rules allow.
