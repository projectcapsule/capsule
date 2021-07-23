# Block access to multitenant resources

**Profile Applicability:** L1

**Type:** Behavioral

**Category:** Tenant Isolation

**Description:** Each tenant namespace may contain resources setup by the cluster administrator for multi-tenancy, such as role bindings, and network policies. Tenants should not be allowed to modify the namespaced resources created by the cluster administrator for multi-tenancy. However, for some resources such as network policies, tenants can configure additional instances of the resource for their workloads.

**Rationale:** Tenants can escalate priviliges and impact other tenants if they are able to delete or modify required multi-tenancy resources such as namespace resource quotas or default network policy.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta1
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

However, due the additive nature of networkpolicies, the `DENY ALL` policy set by the cluster admin, prevents the hijacking.

As tenant owner list RBAC permissions set by Capsule

```bash 
kubectl --kubeconfig alice get rolebindings
NAME                ROLE                                    AGE
namespace-deleter   ClusterRole/capsule-namespace-deleter   11h
namespace:admin     ClusterRole/admin                       11h
```

As tenant owner, try to change/delete  the rolebindings in order to escalate permissions

```bash 
kubectl --kubeconfig alice edit/delete rolebinding namespace:admin
```

The rolebindings is immediately recreated by Capsule:

```
kubectl --kubeconfig alice get rolebindings
NAME                ROLE                                    AGE
namespace-deleter   ClusterRole/capsule-namespace-deleter   11h
namespace:admin     ClusterRole/admin                       2s
```

However, the tenant owner can create and assign permissions inside namespace she owns

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