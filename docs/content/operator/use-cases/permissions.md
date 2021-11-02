# Assign permissions
Alice acts as the tenant admin. Other users can operate inside the tenant with different levels of permissions and authorizations. Alice is responsible for creating additional roles and assigning these roles to other users to work in the same tenant.

One of the key design principles of the Capsule is self-provisioning management from the tenant owner's perspective. Alice, the tenant owner, does not need to interact with Bill, the cluster admin, to complete her day-by-day duties. On the other side, Bill does not have to deal with multiple requests coming from multiple tenant owners that probably will overwhelm him.

Capsule leaves Alice, and the other tenant owners, the freedom to create RBAC roles at the namespace level, or using the pre-defined cluster roles already available in Kubernetes. Since roles and rolebindings are limited to a namespace scope, Alice can assign the roles to the other users accessing the same tenant only after the namespace is created. This gives Alice the power to administer the tenant without the intervention of the cluster admin.

From the cluster admin perspective, the only required action for Bill is to provide the other identities, eg. `joe` in the Identity Management system. This task can be done once when onboarding the tenant and the number of users accessing the tenant can be part of the tenant business profile.

Alice can create Roles and RoleBindings only in the namespaces she owns

```
kubectl auth can-i get roles -n oil-development
yes

kubectl auth can-i get rolebindings -n oil-development
yes
```

so she can assign the role of namespace `oil-development` admin to Joe, another user accessing the tenant `oil`

```yaml
kubectl --as alice --as-group capsule.clastix.io apply -f - << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  labels:
  name: oil-development:admin
  namespace: oil-development
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- apiGroup: rbac.authorization.k8s.io
  kind: User
  name: joe
EOF
```

Joe now can operate on the namespace `oil-development` as admin but he has no access to the other namespaces `oil-production`, and `oil-test` that are part of the same tenant:

```
kubectl --as joe --as-group capsule.clastix.io auth can-i create pod -n oil-development
yes

kubectl --as joe --as-group capsule.clastix.io auth can-i create pod -n oil-production
no
```

> Please, note the user `joe`, in the example above, is not acting as tenant owner. He can just operate in `oil-development` namespace as admin.

# What’s next
See how Bill, the cluster admin, sets resources quota and limits for Alice's tenant. [Enforce resources quota and limits](/docs/operator/use-cases/resources-quota-limits).
