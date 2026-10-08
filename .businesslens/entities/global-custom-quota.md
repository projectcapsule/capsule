---
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/globalcustomquota_types.go" }
---

# GlobalCustomQuota

A limit on any quantity, counted or summed from fields of chosen kinds of objects across selected namespaces.

## Information kept

- **Limit** — the most it allows
- **Namespace selectors** — the namespaces it covers; all namespaces when empty
- **Sources** — the kinds of objects counted and the field or expression each contributes, added or subtracted
- **Scope selectors** — labels an object needs to count
- **Used** — current usage
- **Available** — what remains
- **Contributors** — each object counted and its share
