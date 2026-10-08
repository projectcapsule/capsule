---
availability:
  - { place: kubernetes-api::cluster-administration }
references:
  - { kind: code, role: implementation, target: "api/v1beta2/capsuleconfiguration_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/cfg/validation.go" }
  - { kind: code, role: implementation, target: "internal/controllers/cfg/status/manager.go" }
---

# Edit Capsule configuration

An administrator changes the CapsuleConfiguration: Capsule users and administrators, ignored groups, the namespace prefix rule and protected names, promotion, the cluster roles Capsule grants, forbidden node metadata and default identities.
