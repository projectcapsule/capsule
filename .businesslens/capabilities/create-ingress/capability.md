---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_class.go" }
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_hostnames.go" }
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_collision.go" }
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_wildcard.go" }
  - { kind: code, role: implementation, target: "internal/webhook/defaults/ingress.go" }
  - { kind: code, role: implementation, target: "internal/webhook/rules/generic/validation/ingress.go" }
---

# Create Ingress

A tenant owner creates an Ingress in a tenant namespace, admitted only with an allowed class and hostnames that are allowed and not already taken within the tenant's collision scope.
