# Block add capabilities

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
apiVersion: capsule.clastix.io/v1beta1
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

You should have the pod blocked by PodSecurityPolicy.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```