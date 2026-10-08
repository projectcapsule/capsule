---
type: cli
actors: [tenant-owner, permit-approver]
references:
  - { kind: code, role: implementation, target: "cmd/cli/cmd/root.go" }
  - { kind: code, role: implementation, target: "cmd/cli/cmd/resourcepermit/root.go" }
  - { kind: code, role: context, target: ".goreleaser.yml" }
---

# Capsule CLI

The Capsule command-line tool, also usable as a kubectl plugin, for reviewing and moving ResourcePermits through
their lifecycle with the caller's kubeconfig identity.
