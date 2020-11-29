# Assign permissions roles in a tenant
Alice acts as the tenant admin. Other users can operate inside the tenant with different levels of permissions and authorizations. Alice is responsible for creating additional roles and assigning these roles to other users to work in the same tenant.

One of the key design principles of the Capsule is the self-provisioning management from the tenant owner's perspective. Alice, the tenant owner, does not need to interact with Bill, the cluster admin, to complete her day-by-day duties. On the other side, Bill has not to deal with multiple requests coming from multiple tenant owners that probably will overwhelm him.

Capsule leaves Alice the freedom to create RBAC roles at the namespace level (or using the pre-defined roles already available in Kubernetes) and assign them to other users in the tenant according to needs and requirements. Being roles and rolebindings, limited to a namespace scope, Alice can assign the roles to the other users accessing the same tenant only after a namespace is created. This gives Alice the power to admin the tenant without asking the cluster admin.

From the cluster admin perspective, the only required action to Bill is to provision the other identities in the Identity Management system of Acme Corp. But this task can be done once, when onboarding a new tenant in the system and the users accessing the tenant can be part of the tenant business profile.

As stated before, Capsule does not care about the authentication strategy used in the cluster and all the Kubernetes methods of [authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/) are supported. The only requirement to use Capsule is Bill has to assign all the tenant identities to the `capsule.clastix.io` group.

Alice can create Roles and RoleBindings in each of the namespaces she created

```
alice@caas# kubectl auth can-i get roles
no

alice@caas# kubectl auth can-i get roles -n oil-development
yes

alice@caas# kubectl auth can-i get rolebindings -n oil-development
yes

```

so she can assign the role of namespace `oil-development` admin to Joe, another user accessing the tenant `oil`

```yaml
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
```

Joe now can operate on the namespace `oil-development` as admin but he has no access to the other namespaces `oil-production`, and `oil-test` that are part of the same tenant. 

# What’s next
See how Bill, the cluster admin, set resources quota and limits for Alice's tenant. [Resources quota and limits enforcement for a tenant]().