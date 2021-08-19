# Require run as non-root user

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
