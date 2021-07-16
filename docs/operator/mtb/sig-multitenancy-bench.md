# Meet the multi-tenancy benchmark MTB
Actually, there's no yet a real standard for the multi-tenancy model in Kubernetes, although the [SIG multi-tenancy group](https://github.com/kubernetes-sigs/multi-tenancy) is working on that. SIG multi-tenancy drafted a generic validation schema appliable to generic multi-tenancy projects. Multi-Tenancy Benchmarks [MTB](https://github.com/kubernetes-sigs/multi-tenancy/tree/master/benchmarks) are guidelines for multi-tenant configuration of Kubernetes clusters. Capsule is an open source multi-tenacy operator and we decided to meet the requirements of MTB.

> N.B. At time of writing, the MTB are in development and not ready for usage. Strictly speaking, we do not claim an official conformance to MTB, but just to adhere to the multi-tenancy requirements and best practices promoted by MTB.

|MTB Benchmark |MTB Profile|Capsule Version|Conformance|Notes  |
|--------------|-----------|---------------|-----------|-------|
|[Block access to cluster resources](block-access-to-cluster-resources.md)|L1|v0.1.0|✓|---|
|[Block access to multitenant resources](block-access-to-multitenant-resources.md)|L1|v0.1.0|✓|---|
|[Block access to other tenant resources](block-access-to-other-tenant-resources.md)|L1|v0.1.0|✓|MTB draft|
|[Block add capabilities](block-add-capabilities.md)|L1|v0.1.0|✓|---|
|[Require always imagePullPolicy](require-always-imagepullpolicy.md)|L1|v0.1.0|✓|---|
|Require run as non-root user|L1|v0.1.0|✓|---|
|Block privileged containers|L1|v0.1.0|✓|---|
|Block privilege escalation|L1|v0.1.0|✓|---|
|Configure namespace resource quotas|L1|v0.1.0|✓|---|
|Configure namespace object limits|L1|v0.1.0|✓|---|
|Block use of host path volumes|L1|v0.1.0|✓|---|
|Block use of NodePort services|L1|v0.1.0|✓|---|
|Block use of host networking and ports|L1|v0.1.0|✓|---|
|Block use of host PID|L1|v0.1.0|✓|---|
|Block use of host IPC|L1|v0.1.0|✓|---|
|Block modification of resource quotas|L1|v0.1.0|✓|---|
|Require PersistentVolumeClaim for storage|L1|v0.1.0|✓|MTB draft|
|Require PV reclaim policy of delete|L1|v0.1.0|✓|MTB draft|
|Block use of existing PVs|L1|v0.1.0|✓|MTB draft|
|Block network access across tenant namespaces|L1|v0.1.0|✓|MTB draft|
|Allow self-service management of Network PoliciesL2|v0.1.0|✓|---|
|Allow self-service management of RolesL2|v0.1.0|✓|MTB draft|
|Allow self-service management of Roles Bindings|L2|v0.1.0|✓|MTB draft|
