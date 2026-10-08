---
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/customquota_types.go" }
  - { kind: code, role: implementation, target: "internal/controllers/customquotas/custom_quota_controller.go" }
---

# CustomQuota

A limit on any quantity, counted or summed from fields of chosen kinds of objects in one namespace.

## Information kept

- **Limit** — the most it allows
- **Sources** — the kinds of objects counted and the field or expression each contributes, added or subtracted
- **Scope selectors** — labels an object needs to count
- **Used** — current usage
- **Available** — what remains
- **Contributors** — each object counted and its share
