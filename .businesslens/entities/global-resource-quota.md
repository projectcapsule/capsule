---
domain: resource-management
references:
  - { kind: code, role: implementation, target: "api/v1beta2/globalresourcequota_types.go" }
  - { kind: code, role: implementation, target: "internal/controllers/globalresourcequotas/controller.go" }
---

# GlobalResourceQuota

A resource quota shared by all namespaces it selects, across tenants when an administrator wants it, or within one tenant when a namespace rule generates it.

## Information kept

- **Namespace selectors** — the namespaces it covers; all namespaces when empty
- **Hard limits** — the resource limits shared by those namespaces
- **Used** — the total used across them
- **Available** — what remains
- **Namespace usage** — what each namespace uses
