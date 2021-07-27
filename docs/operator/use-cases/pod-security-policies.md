# Assign Pod Security Policies
Bill, the cluster admin, can assign a dedicated Pod Security Policy (PSP) to the Alice's tenant. This is likely to be a requirement in a multi-tenancy environment.

The cluster admin creates a PSP:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: psp:restricted
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  ...
EOF
```

Then create a _ClusterRole_ using or granting the said item

```yaml
kubectl -n oil-production apply -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: psp:restricted
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['psp:restricted']
  verbs: ['use']
EOF
```

Bill can assign this role to all namespaces in the Alice's tenant by setting it in the tenant manifest:

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
  additionalRoleBindings:
  - clusterRoleName: psp:privileged
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF
```

With the given specification, Capsule will ensure that all Alice's namespaces will contain a _RoleBinding_ for the specified _Cluster Role_.

For example, in the `oil-production` namespace, Alice will see:

```yaml
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: 'capsule-oil-psp:privileged'
  namespace: oil-production
  labels:
    capsule.clastix.io/role-binding: a10c4c8c48474963
    capsule.clastix.io/tenant: oil
subjects:
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: 'system:authenticated'
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: 'psp:privileged'
```

With the above example, Capsule is forbidding any authenticated user in `oil-production` namespace to run privileged pods and to perform privilege escalation as declared by the Cluster Role `psp:privileged`.

# What’s next
See how Bill, the cluster admin, can assign to Alice the permissions to create custom resources in her tenant. [Create Custom Resources](./custom-resources.md).