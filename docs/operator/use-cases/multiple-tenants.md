# Assign multiple tenants to an owner
In some scenarios, a single team is likely responsible for multiple lines of business. For example, in our sample organization Acme Corp., Alice is responsible for both the Oil and Gas lines of business. It's more likely that Alice requires two different tenants, for example, `oil` and `gas` to keep things isolated.

By design, the Capsule operator does not permit a hierarchy of tenants, since all tenants are at the same levels. However, we can assign the ownership of multiple tenants to the same user or group of users.

Bill, the cluster admin, creates multiple tenants having `alice` as owner:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
EOF
```

and

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - name: alice
    kind: User
EOF
```

Alternatively, the ownership can be assigned to a group called `oil-and-gas`:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: oil-and-gas
    kind: Group
EOF
```

and

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - name: oil-and-gas
    kind: Group
EOF
```

The two tenants remain isolated from each other in terms of resources assignments, e.g. _ResourceQuota_, _Nodes Pool_, _Storage Calsses_ and _Ingress Classes_, and in terms of governance, e.g. _NetworkPolicies_, _PodSecurityPolicies_, _Trusted Registries_, etc.


When Alice logs in, she has access to all namespaces belonging to both the `oil` and `gas` tenants.

```
kubectl create ns oil-production
kubectl create ns gas-production
```

When the enforcement of the naming convention with the `--force-tenant-prefix` option, is enabled, the namespaces are automatically assigned to the right tenant by Capsule because the operator does a lookup on the tenant names. If the `--force-tenant-prefix` option, is not set,   Alice needs to specify the tenant name as a label `capsule.clastix.io/tenant=<desired_tenant>` in the namespace manifest:

```yaml
kubectl apply -f - << EOF
kind: Namespace
apiVersion: v1
metadata:
  name: gas-production
  labels:
    capsule.clastix.io/tenant: gas
EOF
```

> If not specified, Capsule will deny with the following message:
>`Unable to assign namespace to tenant. Please use capsule.clastix.io/tenant label when creating a namespace.`

# What’s next

See how Bill, the cluster admin, can cordon all the Namespaces belonging to a Tenant. [Cordoning a Tenant](./cordoning-tenant.md).
