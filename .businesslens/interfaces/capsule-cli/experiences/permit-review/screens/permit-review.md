---
entities:
  - entity: resource-permit
    shows: [Duration, Keep for, Rendered resources]
    collects: [Review, Duration, Start time, Keep for]
references:
  - { kind: code, role: implementation, target: "cmd/cli/cmd/resourcepermit/review.go" }
  - { kind: code, role: implementation, target: "cmd/cli/cmd/resourcepermit/utils.go#printResourcePermitsApprovalTable" }
---

# Permit review

One requested ResourcePermit under review: how long it will last and be kept, and each rendered resource with the policy it is created under. The reviewer approves or denies it here, optionally with a message and a changed duration, start time or retention.
