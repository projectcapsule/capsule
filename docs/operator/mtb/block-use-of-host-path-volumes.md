# Block use of host path volumes

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Host Protection

**Description:** Tenants should not be able to mount host volumes and directories.

**Rationale:** The use of host volumes and directories can be used to access shared data or escalate priviliges and also creates a tight coupling between a tenant workload and a host.

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
apiVersion: capsule.clastix.io/v1beta1
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

You should have the pod blocked by `PodSecurityPolicy`.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete PodSecurityPolicy tenant
kubectl --kubeconfig cluster-admin delete ClusterRole tenant:psp
```
