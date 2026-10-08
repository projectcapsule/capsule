---
references:
  - { kind: code, role: implementation, target: "pkg/api/rules/enforce_workloads_types.go" }
  - { kind: code, role: implementation, target: "internal/webhook/pod/handler.go" }
---

# Workload

A pod or a pod controller (Deployment, StatefulSet, DaemonSet, ReplicaSet, ReplicationController, Job, CronJob) in a tenant namespace.

## Information kept

- **Kind** — pod or the kind of controller
- **Images** — the container, init container, ephemeral container and image volume images
- **Image pull policies** — the pull policy of each container
- **Priority class** — its priority class
- **Runtime class** — its runtime class
- **Placement** — scheduler, node selector, tolerations, topology spread constraints and affinity
- **Security profiles** — seccomp and AppArmor profiles, read-only root filesystem and host user namespace
- **QoS class** — its quality-of-service class
- **Resources** — container resource requests and limits
- **Image pull secrets** — secrets referenced for pulling images
- **Labels** — its labels
- **Annotations** — its annotations
