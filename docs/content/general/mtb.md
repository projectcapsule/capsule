# Multi-Tenancy Benchmark

The [Multi-Tenancy Benchmark](https://github.com/kubernetes-sigs/multi-tenancy) is a _WG_ (Working Group) committed to achieving multi-tenancy in Kubernetes.

The _Benchmarks_ are guidelines that validate if a Kubernetes cluster is properly configured for multi-tenancy.

**Capsule** is an open source multi-tenancy operator, we decided to meet the requirements of _MTB_. although at the time of writing, it's in development and not ready for usage.
Strictly speaking, we do not claim official conformance to _MTB_, but just to adhere to the multi-tenancy requirements and best practices promoted by _MTB_.

|MTB Benchmark |MTB Profile|Capsule Version|Conformance|Notes  |
|--------------|-----------|---------------|-----------|-------|
|[Block access to cluster resources](#block-access-to-cluster-resources)|L1|v0.1.0|✓|---|
|[Block access to multitenant resources](#block-access-to-multitenant-resources)|L1|v0.1.0|✓|---|
|[Block access to other tenant resources](#block-access-to-other-tenant-resources)|L1|v0.1.0|✓|MTB draft|
|[Block add capabilities](#block-add-capabilities)|L1|v0.1.0|✓|---|
|[Require always imagePullPolicy](#require-always-imagepullpolicy)|L1|v0.1.0|✓|---|
|[Require run as non-root user](#require-run-as-non-root-user)|L1|v0.1.0|✓|---|
|[Block privileged containers](#block-privileged-containers)|L1|v0.1.0|✓|---|
|[Block privilege escalation](#block-privilege-escalation)|L1|v0.1.0|✓|---|
|[Configure namespace resource quotas](#configure-namespace-resource-quotas)|L1|v0.1.0|✓|---|
|[Block modification of resource quotas](#block-modification-of-resource-quotas)|L1|v0.1.0|✓|---|
|[Configure namespace object limits](#configure-namespace-object-limits)|L1|v0.1.0|✓|---|
|[Block use of host path volumes](#block-use-of-host-path-volumes)|L1|v0.1.0|✓|---|
|[Block use of host networking and ports](#block-use-of-host-networking-and-ports)|L1|v0.1.0|✓|---|
|[Block use of host PID](#block-use-of-host-pid)|L1|v0.1.0|✓|---|
|[Block use of host IPC](#block-use-of-host-ipc)|L1|v0.1.0|✓|---|
|[Block use of NodePort services](#block-use-of-nodeport-services)|L1|v0.1.0|✓|---|
|[Require PersistentVolumeClaim for storage](#require-persistentvolumeclaim-for-storage)|L1|v0.1.0|✓|MTB draft|
|[Require PV reclaim policy of delete](#require-pv-reclaim-policy-of-delete)|L1|v0.1.0|✓|MTB draft|
|[Block use of existing PVs](#block-use-of-existing-pvs)|L1|v0.1.0|✓|MTB draft|
|[Block network access across tenant namespaces](#block-network-access-across-tenant-namespaces)|L1|v0.1.0|✓|MTB draft|
|[Allow self-service management of Network Policies](#allow-self-service-management-of-network-policies)|L2|v0.1.0|✓|---|
|[Allow self-service management of Roles](#allow-self-service-management-of-roles)|L2|v0.1.0|✓|MTB draft|
|[Allow self-service management of Role Bindings](#allow-self-service-management-of-role-bindings)|L2|v0.1.0|✓|MTB draft|

## Allow self-service management of Network Policies

**Profile Applicability:** L2

**Type:** Behavioral

**Category:** Self-Service Operations

**Description:** Tenants should be able to perform self-service operations by creating their own network policies in their namespaces.

**Rationale:** Enables self-service management of network-policies.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  networkPolicies:
    items:
    - ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: oil
      podSelector: {}
      policyTypes:
      - Egress
      - Ingress
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, retrieve the networkpolicies resources in the tenant namespace

```bash 
kubectl --kubeconfig alice get networkpolicies 
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         7m5s
```

As a tenant, checks for permissions to manage networkpolicy for each verb

```bash 
kubectl --kubeconfig alice auth can-i get networkpolicies
kubectl --kubeconfig alice auth can-i create networkpolicies
kubectl --kubeconfig alice auth can-i update networkpolicies
kubectl --kubeconfig alice auth can-i patch networkpolicies
kubectl --kubeconfig alice auth can-i delete networkpolicies
kubectl --kubeconfig alice auth can-i deletecollection networkpolicies
```

Each command must return 'yes'

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Allow self-service management of Role Bindings

**Profile Applicability:** L2

**Type:** Behavioral

**Category:** Self-Service Operations

**Description:** Tenants should be able to perform self-service operations by creating their rolebindings in their namespaces.

**Rationale:** Enables self-service management of roles.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner check for permissions to manage rolebindings for each verb

```bash 
kubectl --kubeconfig alice auth can-i get rolebindings
kubectl --kubeconfig alice auth can-i create rolebindings
kubectl --kubeconfig alice auth can-i update rolebindings
kubectl --kubeconfig alice auth can-i patch rolebindings
kubectl --kubeconfig alice auth can-i delete rolebindings
kubectl --kubeconfig alice auth can-i deletecollection rolebindings
```

Each command must return 'yes'

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Allow self-service management of Roles

**Profile Applicability:** L2

**Type:** Behavioral

**Category:** Self-Service Operations

**Description:** Tenants should be able to perform self-service operations by creating their own roles in their namespaces.

**Rationale:** Enables self-service management of roles.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, check for permissions to manage roles for each verb

```bash 
kubectl --kubeconfig alice auth can-i get roles
kubectl --kubeconfig alice auth can-i create roles
kubectl --kubeconfig alice auth can-i update roles
kubectl --kubeconfig alice auth can-i patch roles
kubectl --kubeconfig alice auth can-i delete roles
kubectl --kubeconfig alice auth can-i deletecollection roles
```

Each command must return 'yes'

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Block access to cluster resources

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Control Plane Isolation

**Description:** Tenants should not be able to view, edit, create or delete cluster (non-namespaced) resources such Node, ClusterRole, ClusterRoleBinding, etc.

**Rationale:** Access controls should be configured for tenants so that a tenant cannot list, create, modify or delete cluster resources

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil
```

As cluster admin, run the following command to retrieve the list of non-namespaced resources
```bash 
kubectl --kubeconfig cluster-admin api-resources --namespaced=false
```
For all non-namespaced resources, and each verb (get, list, create, update, patch, watch, delete, and deletecollection) issue the following command:

```bash 
kubectl --kubeconfig alice auth can-i <verb> <resource>
```
Each command must return `no`

**Exception:**

It should, but it does not:

```bash 
kubectl --kubeconfig alice auth can-i create selfsubjectaccessreviews
yes
kubectl --kubeconfig alice auth can-i create selfsubjectrulesreviews
yes
kubectl --kubeconfig alice auth can-i create namespaces
yes
```

Any kubernetes user can create `SelfSubjectAccessReview` and `SelfSubjectRulesReviews` to checks whether he/she can act. First, two exceptions are not an issue.

```bash 
kubectl --anyuser auth can-i --list
Resources                                       Non-Resource URLs   Resource Names   Verbs
selfsubjectaccessreviews.authorization.k8s.io   []                  []               [create]
selfsubjectrulesreviews.authorization.k8s.io    []                  []               [create]
                                                [/api/*]            []               [get]
                                                [/api]              []               [get]
                                                [/apis/*]           []               [get]
                                                [/apis]             []               [get]
                                                [/healthz]          []               [get]
                                                [/healthz]          []               [get]
                                                [/livez]            []               [get]
                                                [/livez]            []               [get]
                                                [/openapi/*]        []               [get]
                                                [/openapi]          []               [get]
                                                [/readyz]           []               [get]
                                                [/readyz]           []               [get]
                                                [/version/]         []               [get]
                                                [/version/]         []               [get]
                                                [/version]          []               [get]
                                                [/version]          []               [get]
```

To enable namespace self-service provisioning, Capsule intentionally gives permissions to create namespaces to all users belonging to the Capsule group:

```bash
kubectl describe clusterrolebindings capsule-namespace-provisioner
Name:         capsule-namespace-provisioner
Labels:       <none>
Annotations:  <none>
Role:
  Kind:  ClusterRole
  Name:  capsule-namespace-provisioner
Subjects:
  Kind   Name                Namespace
  ----   ----                ---------
  Group  capsule.clastix.io  

kubectl describe clusterrole capsule-namespace-provisioner
Name:         capsule-namespace-provisioner
Labels:       <none>
Annotations:  <none>
PolicyRule:
  Resources   Non-Resource URLs  Resource Names  Verbs
  ---------   -----------------  --------------  -----
  namespaces  []                 []              [create]
```

Capsule controls self-service namespace creation by limiting the number of namespaces the user can create by the `tenant.spec.namespaceQuota option`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Block access to multitenant resources

**Profile Applicability:** L1

**Type:** Behavioral

**Category:** Tenant Isolation

**Description:** Each tenant namespace may contain resources set up by the cluster administrator for multi-tenancy, such as role bindings, and network policies. Tenants should not be allowed to modify the namespaced resources created by the cluster administrator for multi-tenancy. However, for some resources such as network policies, tenants can configure additional instances of the resource for their workloads.

**Rationale:** Tenants can escalate privileges and impact other tenants if they can delete or modify required multi-tenancy resources such as namespace resource quotas or default network policy.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  networkPolicies:
    items:
    - podSelector: {}
      policyTypes:
      - Ingress
      - Egress
    - egress:
      - to:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: oil
      ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: oil
      podSelector: {}
      policyTypes:
      - Egress
      - Ingress
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, retrieve the networkpolicies resources in the tenant namespace

```bash 
kubectl --kubeconfig alice get networkpolicies 
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         7m5s
capsule-oil-1   <none>         7m5s
```

As tenant owner try to modify or delete one of the networkpolicies

```bash 
kubectl --kubeconfig alice delete networkpolicies capsule-oil-0
```

You should receive an error message denying the edit/delete request

```bash 
Error from server (Forbidden): networkpolicies.networking.k8s.io "capsule-oil-0" is forbidden:
User "oil" cannot delete resource "networkpolicies" in API group "networking.k8s.io" in the namespace "oil-production"
```

As tenant owner, you can create an additional networkpolicy inside the namespace

```yaml
kubectl create -f - << EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hijacking
  namespace: oil-production
spec:
  egress: 
    - to:
      - ipBlock:
          cidr: 0.0.0.0/0
  podSelector: {}
  policyTypes:
  - Egress
EOF
```

However, due to the additive nature of networkpolicies, the `DENY ALL` policy set by the cluster admin, prevents hijacking.

As tenant owner list RBAC permissions set by Capsule

```bash 
kubectl --kubeconfig alice get rolebindings
NAME                                      ROLE                                    AGE
capsule-oil-0-admin                       ClusterRole/admin                       11h
capsule-oil-1-capsule-namespace-deleter   ClusterRole/capsule-namespace-deleter   11h
```

As tenant owner, try to change/delete  the rolebinding to escalate permissions

```bash 
kubectl --kubeconfig alice edit/delete rolebinding capsule-oil-0-admin
```

The rolebinding is immediately recreated by Capsule:

```
kubectl --kubeconfig alice get rolebindings
NAME                                      ROLE                                    AGE
capsule-oil-0-admin                       ClusterRole/admin                       2s
capsule-oil-1-capsule-namespace-deleter   ClusterRole/capsule-namespace-deleter   11h
```

However, the tenant owner can create and assign permissions inside the namespace she owns

```yaml
kubectl create -f - << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  labels:
  name: oil-robot:admin
  namespace: oil-production
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- kind: ServiceAccount
  name: default
  namespace: oil-production
EOF
```


**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Block access to other tenant resources

**Profile Applicability:** L1

**Type:** Behavioral

**Category:** Tenant Isolation

**Description:** Each tenant has its own set of resources, such as namespaces, service accounts, secrets, pods, services, etc. Tenants should not be allowed to access each other's resources.

**Rationale:** Tenant's resources must be not accessible by other tenants.

**Audit:**

As cluster admin, create a couple of tenants

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil

```

and

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - kind: User
    name: joe
EOF

./create-user.sh joe gas

```

As `oil` tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As `gas` tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig joe create ns gas-production
kubectl --kubeconfig joe config set-context --current --namespace gas-production
```


As `oil` tenant owner, try to retrieve the resources in the `gas` tenant namespaces

```bash 
kubectl --kubeconfig alice get serviceaccounts --namespace  gas-production 
```

You must receive an error message:

```
Error from server (Forbidden): serviceaccount is forbidden:
User "oil" cannot list resource "serviceaccounts" in API group "" in the namespace "gas-production"
```

As `gas` tenant owner, try to retrieve the resources in the `oil` tenant namespaces

```bash 
kubectl --kubeconfig joe get serviceaccounts --namespace  oil-production 
```

You must receive an error message:

```
Error from server (Forbidden): serviceaccount is forbidden:
User "joe" cannot list resource "serviceaccounts" in API group "" in the namespace "oil-production"
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenants oil gas
```

## Block add capabilities

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Control Plane Isolation

**Description:** Control Linux capabilities.

**Rationale:** Linux allows defining fine-grained permissions using capabilities. With Kubernetes, it is possible to add capabilities for pods that escalate the level of kernel access and allow other potentially dangerous behaviors.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` with `allowedCapabilities` and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  # The default set of capabilities are implicitly allowed
  # The empty set means that no additional capabilities may be added beyond the default set
  allowedCapabilities: []
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  namespace: oil-production
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod and see new capabilities cannot be added in the tenant namespaces

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-settime-cap
  namespace:
  labels:
spec:
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
    securityContext:
      capabilities:
        add:
        - SYS_TIME
EOF
```

You must have the pod blocked by PodSecurityPolicy.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```

## Block modification of resource quotas

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Tenant Isolation

**Description:** Tenants should not be able to modify the resource quotas defined in their namespaces

**Rationale:** Resource quotas must be configured for isolation and fairness between tenants. Tenants should not be able to modify existing resource quotas as they may exhaust cluster resources and impact other tenants.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  resourceQuotas:
    items:
    - hard:
        limits.cpu: "8"
        limits.memory: 16Gi
        requests.cpu: "8"
        requests.memory: 16Gi
    - hard:
        pods: "10"
        services: "50"
    - hard:
        requests.storage: 100Gi
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, check the permissions to modify/delete the quota in the tenant namespace:

```bash 
kubectl --kubeconfig alice auth can-i create quota
kubectl --kubeconfig alice auth can-i update quota
kubectl --kubeconfig alice auth can-i patch quota
kubectl --kubeconfig alice auth can-i delete quota
kubectl --kubeconfig alice auth can-i deletecollection quota
```

Each command must return 'no'

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Block network access across tenant namespaces

**Profile Applicability:** L1

**Type:** Behavioral

**Category:** Tenant Isolation

**Description:** Block network traffic among namespaces from different tenants.

**Rationale:** Tenants cannot access services and pods in another tenant's namespaces.

**Audit:**

As cluster admin, create a couple of tenants

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  networkPolicies:
    items:
    - ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: oil
      podSelector: {}
      policyTypes:
      - Ingress
EOF

./create-user.sh alice oil
```

and

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - kind: User
    name: joe
  networkPolicies:
    items:
    - ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: gas
      podSelector: {}
      policyTypes:
      - Ingress
EOF

./create-user.sh joe gas
```

As `oil` tenant owner, run the following commands to create a namespace and resources in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
kubectl --kubeconfig alice run webserver --image nginx:latest
kubectl --kubeconfig alice expose pod webserver --port 80
```

As `gas` tenant owner, run the following commands to create a namespace and resources in the given tenant

```bash 
kubectl --kubeconfig joe create ns gas-production
kubectl --kubeconfig joe config set-context --current --namespace gas-production
kubectl --kubeconfig joe run webserver --image nginx:latest
kubectl --kubeconfig joe expose pod webserver --port 80
```

As `oil` tenant owner, verify you can access the service in `oil` tenant namespace but not in the `gas` tenant namespace

```bash 
kubectl --kubeconfig alice exec webserver -- curl http://webserver.oil-production.svc.cluster.local
kubectl --kubeconfig alice exec webserver -- curl http://webserver.gas-production.svc.cluster.local
```

Viceversa, as `gas` tenant owner, verify you can access the service in `gas` tenant namespace but not in the `oil` tenant namespace

```bash 
kubectl --kubeconfig alice exec webserver -- curl http://webserver.oil-production.svc.cluster.local
kubectl --kubeconfig alice exec webserver -- curl http://webserver.gas-production.svc.cluster.local
```


**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenants oil gas
```

## Block privilege escalation

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Control Plane Isolation

**Description:** Control container permissions.

**Rationale:** The security `allowPrivilegeEscalation` setting allows a process to gain more privileges from its parent process. Processes in tenant containers should not be allowed to gain additional privileges.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` that sets `allowPrivilegeEscalation=false` and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod or container that sets `allowPrivilegeEscalation=true` in its `securityContext`.

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-priviliged-mode
  namespace: oil-production
  labels:
spec:
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
    securityContext:
      allowPrivilegeEscalation: true
EOF
```

You must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```

## Block privileged containers

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Control Plane Isolation

**Description:** Control container permissions.

**Rationale:** By default a container is not allowed to access any devices on the host, but a “privileged” container can access all devices on the host. A process within a privileged container can also get unrestricted host access. Hence, tenants should not be allowed to run privileged containers.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` that sets `privileged=false` and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  namespace: oil-production
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod or container that sets privileges in its `securityContext`.

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-priviliged-mode
  namespace:
  labels:
spec:
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
    securityContext:
      privileged: true
EOF
```

You must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```

## Block use of existing PVs

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Data Isolation

**Description:** Avoid a tenant to mount existing volumes`.

**Rationale:** Tenants have to be assured that their Persistent Volumes cannot be reclaimed by other tenants.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil
```

As tenant owner, check if you can access the persistent volumes

```bash 
kubectl --kubeconfig alice auth can-i get persistentvolumes
kubectl --kubeconfig alice auth can-i list persistentvolumes
kubectl --kubeconfig alice auth can-i watch persistentvolumes
```

You must receive for all the requests 'no'.

## Block use of host IPC

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Host Isolation

**Description:** Tenants should not be allowed to share the host's inter-process communication (IPC) namespace.

**Rationale:** The `hostIPC` setting allows pods to share the host's inter-process communication (IPC) namespace allowing potential access to host processes or processes belonging to other tenants.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` that restricts `hostIPC` usage and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  hostIPC: false
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  namespace: oil-production
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod mounting the host IPC namespace.

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-host-ipc
  namespace: oil-production
spec:
  hostIPC: true
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
EOF
```

You must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```

## Block use of host networking and ports

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Host Isolation

**Description:** Tenants should not be allowed to use host networking and host ports for their workloads.

**Rationale:** Using `hostPort` and `hostNetwork` allows tenants workloads to share the host networking stack allowing potential snooping of network traffic across application pods.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` that restricts `hostPort` and `hostNetwork` and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  hostNetwork: false
  hostPorts: [] # empty means no allowed host ports
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  namespace: oil-production
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod using `hostNetwork`

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-hostnetwork
  namespace: oil-production
spec:
  hostNetwork: true
  containers:
  - name: nginx
    image: nginx:latest
    ports:
    - containerPort: 80
EOF
```

As tenant owner, create a pod defining a container using `hostPort`

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-hostport
  namespace: oil-production
spec:
  containers:
  - name: nginx
    image: nginx:latest
    ports:
    - containerPort: 80
      hostPort: 9090
EOF
```

In both the cases above, you must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```
## Block use of host path volumes

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Host Protection

**Description:** Tenants should not be able to mount host volumes and directories.

**Rationale:** The use of host volumes and directories can be used to access shared data or escalate privileges and also creates a tight coupling between a tenant workload and a host.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` that restricts `hostPath` volumes and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  volumes: # hostPath is not permitted
    - 'configMap'
    - 'emptyDir'
    - 'projected'
    - 'secret'
    - 'downwardAPI'
    - 'persistentVolumeClaim'
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  namespace: oil-production
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod defining a volume of type `hostpath`.

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-hostpath-volume
  namespace: oil-production
spec:
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
    volumeMounts:
    - mountPath: /tmp
      name: volume
  volumes:
  - name: volume
    hostPath:
      # directory location on host
      path: /data
      type: Directory
EOF
```

You must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```

## Block use of host PID

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Host Isolation

**Description:** Tenants should not be allowed to share the host process ID (PID) namespace.

**Rationale:** The `hostPID` setting allows pods to share the host process ID namespace allowing potential privilege escalation. Tenant pods should not be allowed to share the host PID namespace.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` that restricts `hostPID` usage and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  hostPID: false
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  namespace: oil-production
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod mounting the host PID namespace.

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-host-pid
  namespace: oil-production
spec:
  hostPID: true
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
EOF
```

You must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```

## Block use of NodePort services

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Host Isolation

**Description:** Tenants should not be able to create services of type NodePort.

**Rationale:** the service type `NodePorts` configures host ports that cannot be secured using Kubernetes network policies and require upstream firewalls. Also, multiple tenants cannot use the same host port numbers.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  serviceOptions:
    allowedServices:
      nodePort: false
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, creates a service in the tenant namespace having service type of `NodePort`

```yaml
kubectl --kubeconfig alice apply -f - << EOF
apiVersion: v1
kind: Service
metadata:
  name: nginx
  labels:
  namespace: oil-production
spec:
  ports:
  - protocol: TCP
    port: 8080
    targetPort: 80
  selector:
    run: nginx
  type: NodePort
EOF
```

You must receive an error message denying the request:

```
Error from server
Error from server (NodePort service types are forbidden for the tenant:
error when creating "STDIN": admission webhook "services.capsule.clastix.io" denied the request:
NodePort service types are forbidden for the tenant: please, reach out to the system administrators
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Configure namespace object limits

**Profile Applicability:** L1

**Type:** Configuration

**Category:** Fairness

**Description:** Namespace resource quotas should be used to allocate, track and limit the number of objects, of a particular type, that can be created within a namespace.

**Rationale:** Resource quotas must be configured for each tenant namespace, to guarantee isolation and fairness across tenants.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  resourceQuotas:
    items:
    - hard:
        pods: 100
        services: 50
        services.loadbalancers: 3
        services.nodeports: 20
        persistentvolumeclaims: 100
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, retrieve the configured quotas in the tenant namespace:

```bash 
kubectl --kubeconfig alice get quota
NAME            AGE   REQUEST                 LIMIT
capsule-oil-0   23s   persistentvolumeclaims: 0/100,
                      pods: 0/100, services: 0/50,
                      services.loadbalancers: 0/3,
                      services.nodeports: 0/20  
```

Make sure that a quota is configured for API objects: `PersistentVolumeClaim`, `LoadBalancer`, `NodePort`, `Pods`, etc

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Configure namespace resource quotas

**Profile Applicability:** L1

**Type:** Configuration

**Category:** Fairness

**Description:** Namespace resource quotas should be used to allocate, track, and limit a tenant's use of shared resources.

**Rationale:** Resource quotas must be configured for each tenant namespace, to guarantee isolation and fairness across tenants.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  resourceQuotas:
    items:
    - hard:
        limits.cpu: "8"
        limits.memory: 16Gi
        requests.cpu: "8"
        requests.memory: 16Gi
    - hard:
        requests.storage: 100Gi
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, retrieve the configured quotas in the tenant namespace:

```bash 
kubectl --kubeconfig alice get quota
NAME            AGE   REQUEST                                      LIMIT
capsule-oil-0   24s   requests.cpu: 0/8, requests.memory: 0/16Gi   limits.cpu: 0/8, limits.memory: 0/16Gi                 
capsule-oil-1   24s   requests.storage: 0/10Gi                     
```

Make sure that a quota is configured for CPU, memory, and storage resources.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Require always imagePullPolicy

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Data Isolation

**Description:** Set the image pull policy to Always for tenant workloads.

**Rationale:** Tenants have to be assured that their private images can only be used by those who have the credentials to pull them.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  imagePullPolicies:
  - Always
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, creates a pod in the tenant namespace having `imagePullPolicies=IfNotPresent`

```yaml
kubectl --kubeconfig alice apply -f - << EOF
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: oil-production
spec:
  containers:
  - name: nginx
    image: nginx:latest
    imagePullPolicy: IfNotPresent
EOF
```

You must receive an error message denying the request:

```
Error from server
(ImagePullPolicy IfNotPresent for container nginx is forbidden, use one of the followings: Always): error when creating "STDIN": admission webhook "pods.capsule.clastix.io" denied the request:
ImagePullPolicy IfNotPresent for container nginx is forbidden, use one of the followings: Always
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```

## Require PersistentVolumeClaim for storage

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** na

**Description:** Tenants should not be able to use all volume types except `PersistentVolumeClaims`.

**Rationale:** In some scenarios, it would be required to disallow usage of any core volume types except PVCs.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` allowing only `PersistentVolumeClaim` volumes and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  volumes: 
    - 'persistentVolumeClaim'
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  namespace: oil-production
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod defining a volume of any of the core type except `PersistentVolumeClaim`. For example:

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-hostpath-volume
  namespace: oil-production
spec:
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
    volumeMounts:
    - mountPath: /tmp
      name: volume
  volumes:
  - name: volume
    hostPath:
      # directory location on host
      path: /data
      type: Directory
EOF
```

You must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```

## Require PV reclaim policy of delete

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Data Isolation

**Description:** Force a tenant to use a Storage Class with `reclaimPolicy=Delete`.

**Rationale:** Tenants have to be assured that their Persistent Volumes cannot be reclaimed by other tenants.

**Audit:**

As cluster admin, create a Storage Class with `reclaimPolicy=Delete`

```yaml
kubectl create -f - << EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: delete-policy
reclaimPolicy: Delete
provisioner: clastix.io/nfs
EOF
```

As cluster admin, create a tenant and assign the above Storage Class

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  storageClasses:
    allowed:
    - delete-policy
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, creates a Persistent Volume Claim in the tenant namespace missing the Storage Class or using any other Storage Class:

```yaml
kubectl --kubeconfig alice apply -f - << EOF
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc
  namespace: oil-production
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 12Gi
EOF
```

You must receive an error message denying the request:

```
Error from server (A valid Storage Class must be used, one of the following (delete-policy)):
error when creating "STDIN": admission webhook "pvc.capsule.clastix.io" denied the request:
A valid Storage Class must be used, one of the following (delete-policy)
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete storageclass delete-policy
```

## Require run as non-root user

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Control Plane Isolation

**Description:** Control container permissions.

**Rationale:** Processes in containers run as the root user (uid 0), by default. To prevent potential compromise of container hosts, specify a least-privileged user ID when building the container image and require that application containers run as non-root users.

**Audit:**

As cluster admin, define a `PodSecurityPolicy` with `runAsUser=MustRunAsNonRoot` and map the policy to a tenant:

```yaml
kubectl create -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: tenant
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  runAsUser:
    # Require the container to run without root privileges.
    rule: MustRunAsNonRoot
  supplementalGroups:
    rule: MustRunAs
    ranges:
      # Forbid adding the root group.
      - min: 1
        max: 65535
  fsGroup:
    rule: MustRunAs
    ranges:
      # Forbid adding the root group.
      - min: 1
        max: 65535
EOF
```

> Note: make sure `PodSecurityPolicy` Admission Control is enabled on the APIs server: `--enable-admission-plugins=PodSecurityPolicy`

Then create a ClusterRole using or granting the said item

```yaml
kubectl create -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: tenant:psp
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['tenant']
  verbs: ['use']
EOF
```

And assign it to the tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  additionalRoleBindings:
  - clusterRoleName: tenant:psp
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, create a pod or container that does not set `runAsNonRoot` to `true` in its `securityContext`, and `runAsUser` must not be set to 0.

```yaml 
kubectl --kubeconfig alice apply -f - << EOF 
apiVersion: v1
kind: Pod
metadata:
  name: pod-run-as-root
  namespace: oil-production
spec:
  containers:
  - name: busybox
    image: busybox:latest
    command: ["/bin/sleep", "3600"]
EOF
```

You must have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```
