---
references:
  - { kind: code, role: implementation, target: "internal/webhook/service/validating.go" }
  - { kind: code, role: implementation, target: "pkg/api/rules/enforce_services_types.go" }
---

# Service

A Kubernetes Service in a tenant namespace.

## Information kept

- **Type** — ClusterIP, NodePort, LoadBalancer or ExternalName
- **External IPs** — external IP addresses
- **Load balancer addresses** — requested load balancer IP and source ranges
- **Node ports** — node ports it exposes
- **External name** — the external hostname of an ExternalName Service
- **Labels** — its labels
- **Annotations** — its annotations
