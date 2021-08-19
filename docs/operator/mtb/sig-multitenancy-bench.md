# Meet the multi-tenancy benchmark MTB
Actually, there's no yet a real standard for the multi-tenancy model in Kubernetes, although the [SIG multi-tenancy group](https://github.com/kubernetes-sigs/multi-tenancy) is working on that. SIG multi-tenancy drafted a generic validation schema appliable to generic multi-tenancy projects. Multi-Tenancy Benchmarks [MTB](https://github.com/kubernetes-sigs/multi-tenancy/tree/master/benchmarks) are guidelines for multi-tenant configuration of Kubernetes clusters. Capsule is an open source multi-tenancy operator and we decided to meet the requirements of MTB.

> N.B. At the time of writing, the MTB is in development and not ready for usage. Strictly speaking, we do not claim official conformance to MTB, but just to adhere to the multi-tenancy requirements and best practices promoted by MTB.

|MTB Benchmark |MTB Profile|Capsule Version|Conformance|Notes  |
|--------------|-----------|---------------|-----------|-------|
|[Block access to cluster resources](block-access-to-cluster-resources.md)|L1|v0.1.0|✓|---|
|[Block access to multitenant resources](block-access-to-multitenant-resources.md)|L1|v0.1.0|✓|---|
|[Block access to other tenant resources](block-access-to-other-tenant-resources.md)|L1|v0.1.0|✓|MTB draft|
|[Block add capabilities](block-add-capabilities.md)|L1|v0.1.0|✓|---|
|[Require always imagePullPolicy](require-always-imagepullpolicy.md)|L1|v0.1.0|✓|---|
|[Require run as non-root user](require-run-as-non-root-user.md)|L1|v0.1.0|✓|---|
|[Block privileged containers](block-privileged-containers.md)|L1|v0.1.0|✓|---|
|[Block privilege escalation](block-privilege-escalation.md)|L1|v0.1.0|✓|---|
|[Configure namespace resource quotas](configure-namespace-resource-quotas.md)|L1|v0.1.0|✓|---|
|[Block modification of resource quotas](block-modification-of-resource-quotas.md)|L1|v0.1.0|✓|---|
|[Configure namespace object limits](configure-namespace-object-limits.md)|L1|v0.1.0|✓|---|
|[Block use of host path volumes](block-use-of-host-path-volumes.md)|L1|v0.1.0|✓|---|
|[Block use of host networking and ports](block-use-of-host-networking-and-ports.md)|L1|v0.1.0|✓|---|
|[Block use of host PID](block-use-of-host-pid.md)|L1|v0.1.0|✓|---|
|[Block use of host IPC](block-use-of-host-ipc.md)|L1|v0.1.0|✓|---|
|[Block use of NodePort services](block-use-of-nodeport-services.md)|L1|v0.1.0|✓|---|
|[Require PersistentVolumeClaim for storage](require-persistentvolumeclaim-for-storage.md)|L1|v0.1.0|✓|MTB draft|
|[Require PV reclaim policy of delete](require-reclaim-policy-of-delete.md)|L1|v0.1.0|✓|MTB draft|
|[Block use of existing PVs](block-use-of-existing-persistent-volumes.md)|L1|v0.1.0|✓|MTB draft|
|[Block network access across tenant namespaces](block-network-access-across-tenant-namespaces.md)|L1|v0.1.0|✓|MTB draft|
|[Allow self-service management of Network Policies](allow-self-service-management-of-network-policies.md)|L2|v0.1.0|✓|---|
|[Allow self-service management of Roles](allow-self-service-management-of-roles.md)|L2|v0.1.0|✓|MTB draft|
|[Allow self-service management of Role Bindings](allow-self-service-management-of-rolebindings.md)|L2|v0.1.0|✓|MTB draft|
