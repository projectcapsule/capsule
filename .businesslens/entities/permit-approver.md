---
kind: person
acts: external
references:
  - { kind: code, role: implementation, target: "pkg/api/resourcepermit/approval_types.go#ApprovalSpec" }
---

# Permit approver

A user or group that reviews ResourcePermits: one of the approvers a permit template names, or, when the template names none, anyone a Kubernetes role lets update ResourcePermit status.
