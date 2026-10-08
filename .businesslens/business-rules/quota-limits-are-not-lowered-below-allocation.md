---
appliesTo:
  - { type: capability, id: edit-resource-pool }
  - { type: capability, id: edit-global-resource-quota }
  - { type: capability, id: edit-namespace-rule }
  - { type: capability, id: edit-custom-quota }
  - { type: capability, id: edit-global-custom-quota }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepool/pool_validation.go" }
  - { kind: code, role: implementation, target: "pkg/runtime/quota/validation.go" }
  - { kind: code, role: implementation, target: "internal/webhook/customquota/customquota_validating.go" }
---

# A quota limit is never lowered below what is already allocated

Pools, GlobalResourceQuotas, rule quotas and custom quotas refuse a lower limit than what claims, usage or reservations already hold.
