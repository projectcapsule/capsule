# Multiple tenants owned by the same user
In some scenarios, it's likely that a single team is responsible for multiple lines of business. For example, in our sample organization Acme Corp., Alice is responsible for both the Oil and Gas lines of business. Ans it's more probable that Alice requires two different tenants, for example `oil` and `gas` to keep things isolated.

By design, the Capsule operator does not permit hierarchy of tenants, since all tenants are at the same levels. However, we can assign the ownership of multiple tenants to the same user or group of users.

Bill, the cluster admin, creates multiple tenants having `alice` as owner:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  namespaceQuota: 3
```

and

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: gas
spec:
  owner:
    name: alice
    kind: User
  namespaceQuota: 9
```

So that

```
bill@caas# kubectl get tenants
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR   AGE
oil    3                 3                 alice        User                         3h
gas    9                 0                 alice        User                         1m
```

Alternatively, the ownership can be assigned to a group called `oil-and-gas`:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: oil-and-gas
    kind: Group
  namespaceQuota: 3
```

and

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: gas
spec:
  owner:
    name: oil-and-gas
    kind: Group
  namespaceQuota: 9
```

So that

```
bill@caas# kubectl get tenants
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR   AGE
oil    3                 3                 oil-and-gas  Group                         3h
gas    9                 0                 oil-and-gas  Group                         1m
```

The two tenants still remain isolated each other in terms of resources assignments, e.g. _ResourceQuota_, _Nodes Pool_, _Storage Calsses_ and _Ingress Classes_, and in terms of governance, e.g. _NetworkPolicies_, _PodSecurityPolicies_, _Trusted Registries_, etc.


When Alice logs in CaaS platform, she has access to all namespaces belonging to both the `oil` and `gas` tenants.

```
alice@caas# kubectl create ns oil-production
alice@caas# kubectl create ns gas-production
```

When the enforcement of the naming convention with the `--force-tenant-prefix` option, is enabled, the namespaces are automatically assigned to the right tenant by Capsule because the operator does a lookups on the tenant names. If the `--force-tenant-prefix` option, is not set,   Alice needs to specify the tenant name as a label `capsule.clastix.io/tenant=<desired_tenant>` in the namespace manifest:

```yaml
cat <<EOF > gas-production-ns.yaml
kind: Namespace
apiVersion: v1
metadata:
  name: gas-production
  labels:
    capsule.clastix.io/tenant: gas
EOF

kubectl create -f gas-production-ns.yaml
```

> If not specified, Capsule will deny with the following message:
>
>`Unable to assign namespace to tenant. Please use capsule.clastix.io/tenant label when creating a namespace.`

# What’s next
See references for all the options available in the Tenant Custom Resouce. [Reference]().