---
appliesTo:
  - { type: capability, id: create-workload }
  - { type: entity, id: global-resource-quota, facts: [Hard limits] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/globalresourcequota/calculation.go" }
---

# Usage in the namespaces a GlobalResourceQuota selects never exceeds its hard limits

Requests that would take the selected namespaces together past a hard limit are refused, including requests admitted concurrently.
