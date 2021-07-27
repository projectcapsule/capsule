# Create Custom Resources
Capsule grants admin permissions to the tenant owners but only limited to their namespaces. To achieve that, it assigns the ClusterRole [admin](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles) to the tenant owner. This ClusterRole does not permit the installation of custom resources in the namespaces.

In order to leave the tenant owner to create Custom Resources in their namespaces, the cluster admin defines a proper Cluster Role. For example:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argoproj-provisioner
rules:
- apiGroups:
  - argoproj.io
  resources:
  - applications
  - appprojects
  verbs:
  - create
  - get
  - list
  - watch
  - update
  - patch
  - delete
EOF
```

Bill can assign this role to any namespace in the Alice's tenant by setting it in the tenant manifest:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  - name: joe
    kind: User
  additionalRoleBindings:
    - clusterRoleName: 'argoproj-provisioner'
      subjects:
        - apiGroup: rbac.authorization.k8s.io
          kind: User
          name: alice
        - apiGroup: rbac.authorization.k8s.io
          kind: User
          name: joe
EOF
```

With the given specification, Capsule will ensure that all Alice's namespaces will contain a _RoleBinding_ for the specified _Cluster Role_. For example, in the `oil-production` namespace, Alice will see:

```yaml
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: capsule-oil-argoproj-provisioner
  namespace: oil-production
subjects:
  - kind: User
    apiGroup: rbac.authorization.k8s.io
    name: alice
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: argoproj-provisioner
```

With the above example, Capsule is leaving the tenant owner to create namespaced custom resources.

> Take Note: a tenant owner having the admin scope on its namespaces only, does not have the permission to create Custom Resources Definitions (CRDs) because this requires a cluster admin permission level. Only Bill, the cluster admin, can create CRDs. This is a known limitation of any multi-tenancy environment based on a single Kubernetes cluster.

# What’s next
See how Bill, the cluster admin, can set taints on the Alice's namespaces. [Taint namespaces](./taint-namespaces.md).
