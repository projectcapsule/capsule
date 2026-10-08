---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/dra/validate.go" }
---

# Create ResourceClaim

A tenant owner requests devices through a ResourceClaim or ResourceClaimTemplate, admitted only for device classes the tenant allows.
