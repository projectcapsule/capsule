# Onboard a new tenant
Bill, the cluster admin, receives a new request from Acme Corp.'s CTO asking for a new tenant to be onboarded and Alice user will be the tenant owner. Bill then assigns Alice's identity of `alice` in the Acme Corp. identity management system. Since Alice is a tenant owner, Bill needs to assign `alice` the Capsule group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

To keep things simple, we assume that Bill just creates a client certificate for authentication using X.509 Certificate Signing Request, so Alice's certificate has `"/CN=alice/O=capsule.clastix.io"`.

Bill creates a new tenant `oil` in the CaaS management portal according to the tenant's profile:

```yaml
kubectl create -f - << EOF
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

Bill checks if the new tenant is created and operational:

```
kubectl get tenant oil
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR   AGE
oil    Active                     0                                 33m
```

> Note that namespaces are not yet assigned to the new tenant.
> The tenant owners are free to create their namespaces in a self-service fashion
> and without any intervention from Bill.

Once the new tenant `oil` is in place, Bill sends the login credentials to Alice.

Alice can log in using her credentials and check if she can create a namespace

```
kubectl auth can-i create namespaces
yes
``` 

or even delete the namespace

```
kubectl auth can-i delete ns -n oil-production
yes
```

However, cluster resources are not accessible to Alice

```
kubectl auth can-i get namespaces
no

kubectl auth can-i get nodes
no

kubectl auth can-i get persistentvolumes
no
```

including the `Tenant` resources

```
kubectl auth can-i get tenants
no
```

## Assign a group of users as tenant owner
In the example above, Bill assigned the ownership of `oil` tenant to `alice` user. If another user, e.g. Bob needs to administer the `oil` tenant, Bill can assign the ownership of `oil` tenant to such user too:

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
  - name: bob
    kind: User
EOF
```

However, it's more likely that Bill assigns the ownership of the `oil` tenant to a group of users instead of a single one. Bill creates a new group account `oil-users` in the Acme Corp. identity management system and then he assigns Alice and Bob identities to the `oil-users` group.

The tenant manifest is modified as in the following:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: oil-users
    kind: Group
EOF
```

With the configuration above, any user belonging to the `oil-users` group will be the owner of the `oil` tenant with the same permissions of Alice. For example, Bob can log in with his credentials and issue

```
kubectl auth can-i create namespaces
yes
```

## Assign a robot account as tenant owner
As GitOps methodology is gaining more and more adoption everywhere, it's more likely that an application (Service Account) should act as Tenant Owner. In Capsule, a Tenant can also be owned by a Kubernetes _ServiceAccount_ identity.

The tenant manifest is modified as in the following:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: oil-users
    kind: Group
  owners:
  - name: system:serviceaccount:default:robot
    kind: ServiceAccount
EOF
```

Bill can create a Service Account called `robot`, for example, in the `default` namespace and leave it to act as Tenant Owner of the `oil` tenant

```
kubectl --as system:serviceaccount:default:robot --as-group capsule.clastix.io auth can-i create namesapces
yes
```

# What’s next
See how a tenant owner, creates new namespaces. [Create namespaces](./create-namespaces.md).
