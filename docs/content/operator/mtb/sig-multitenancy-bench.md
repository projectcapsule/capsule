# Meet the multi-tenancy benchmark MTB
Actually, there's no yet a real standard for the multi-tenancy model in Kubernetes, although the [SIG multi-tenancy group](https://github.com/kubernetes-sigs/multi-tenancy) is working on that. SIG multi-tenancy drafted a generic validation schema appliable to generic multi-tenancy projects. Multi-Tenancy Benchmarks [MTB](https://github.com/kubernetes-sigs/multi-tenancy/tree/master/benchmarks) are guidelines for multi-tenant configuration of Kubernetes clusters. Capsule is an open source multi-tenancy operator and we decided to meet the requirements of MTB.

> N.B. At the time of writing, the MTB is in development and not ready for usage. Strictly speaking, we do not claim official conformance to MTB, but just to adhere to the multi-tenancy requirements and best practices promoted by MTB.

|MTB Benchmark |MTB Profile|Capsule Version|Conformance|Notes  |
|--------------|-----------|---------------|-----------|-------|
|[Block access to cluster resources](/docs/operator/mtb/block-access-to-cluster-resources)|L1|v0.1.0|✓|---|
|[Block access to multitenant resources](/docs/operator/mtb/block-access-to-multitenant-resources)|L1|v0.1.0|✓|---|
|[Block access to other tenant resources](/docs/operator/mtb/block-access-to-other-tenant-resources)|L1|v0.1.0|✓|MTB draft|
|[Block add capabilities](/docs/operator/mtb/block-add-capabilities)|L1|v0.1.0|✓|---|
|[Require always imagePullPolicy](/docs/operator/mtb/require-always-imagepullpolicy)|L1|v0.1.0|✓|---|
|[Require run as non-root user](/docs/operator/mtb/require-run-as-non-root-user)|L1|v0.1.0|✓|---|
|[Block privileged containers](/docs/operator/mtb/block-privileged-containers)|L1|v0.1.0|✓|---|
|[Block privilege escalation](/docs/operator/mtb/block-privilege-escalation)|L1|v0.1.0|✓|---|
|[Configure namespace resource quotas](/docs/operator/mtb/configure-namespace-resource-quotas)|L1|v0.1.0|✓|---|
|[Block modification of resource quotas](/docs/operator/mtb/block-modification-of-resource-quotas)|L1|v0.1.0|✓|---|
|[Configure namespace object limits](/docs/operator/mtb/configure-namespace-object-limits)|L1|v0.1.0|✓|---|
|[Block use of host path volumes](/docs/operator/mtb/block-use-of-host-path-volumes)|L1|v0.1.0|✓|---|
|[Block use of host networking and ports](/docs/operator/mtb/block-use-of-host-networking-and-ports)|L1|v0.1.0|✓|---|
|[Block use of host PID](/docs/operator/mtb/block-use-of-host-pid)|L1|v0.1.0|✓|---|
|[Block use of host IPC](/docs/operator/mtb/block-use-of-host-ipc)|L1|v0.1.0|✓|---|
|[Block use of NodePort services](/docs/operator/mtb/block-use-of-nodeport-services)|L1|v0.1.0|✓|---|
|[Require PersistentVolumeClaim for storage](/docs/operator/mtb/require-persistentvolumeclaim-for-storage)|L1|v0.1.0|✓|MTB draft|
|[Require PV reclaim policy of delete](/docs/operator/mtb/require-reclaim-policy-of-delete)|L1|v0.1.0|✓|MTB draft|
|[Block use of existing PVs](/docs/operator/mtb/block-use-of-existing-persistent-volumes)|L1|v0.1.0|✓|MTB draft|
|[Block network access across tenant namespaces](/docs/operator/mtb/block-network-access-across-tenant-namespaces)|L1|v0.1.0|✓|MTB draft|
|[Allow self-service management of Network Policies](/docs/operator/mtb/allow-self-service-management-of-network-policies)|L2|v0.1.0|✓|---|
|[Allow self-service management of Roles](/docs/operator/mtb/allow-self-service-management-of-roles)|L2|v0.1.0|✓|MTB draft|
|[Allow self-service management of Role Bindings](/docs/operator/mtb/allow-self-service-management-of-rolebindings)|L2|v0.1.0|✓|MTB draft|
