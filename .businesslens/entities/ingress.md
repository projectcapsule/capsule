---
references:
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_class.go" }
  - { kind: code, role: implementation, target: "internal/webhook/ingress/validate_hostnames.go" }
---

# Ingress

A Kubernetes Ingress in a tenant namespace.

## Information kept

- **Ingress class** — its ingress class
- **Hostnames** — the hostnames and paths of its rules, and its TLS hosts
