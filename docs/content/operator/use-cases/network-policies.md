# Assign Network Policies
Kubernetes network policies control network traffic between namespaces and between pods in the same namespace. Bill, the cluster admin, can enforce network traffic isolation between different tenants while leaving to Alice, the tenant owner, the freedom to set isolation between namespaces in the same tenant or even between pods in the same namespace.

To meet this requirement, Bill needs to define network policies that deny pods belonging to Alice's namespaces to access pods in namespaces belonging to other tenants, e.g. Bob's tenant `water`, or in system namespaces, e.g. `kube-system`.

Also, Bill can make sure pods belonging to a tenant namespace cannot access other network infrastructures like cluster nodes, load balancers, and virtual machines running other services.  

Bill can set network policies in the tenant manifest, according to the requirements:

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
  networkPolicies:
    items:
    - policyTypes:
      - Ingress
      - Egress
      egress:
      - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 192.168.0.0/16 
      ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: oil
        - podSelector: {}
        - ipBlock:
            cidr: 192.168.0.0/16
      podSelector: {}
EOF
```

The Capsule controller, watching for namespace creation, creates the Network Policies for each namespace in the tenant.

Alice has access to network policies:

```
kubectl -n oil-production get networkpolicies
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         42h
```

Alice can create, patch, and delete additional network policies within her namespaces

```
kubectl -n oil-production auth can-i get networkpolicies
yes

kubectl -n oil-production auth can-i delete networkpolicies
yes

kubectl -n oil-production auth can-i patch networkpolicies
yes
```

For example, she can create

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  labels:
  name: production-network-policy
  namespace: oil-production
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  - Egress
EOF
```

Check all the network policies

```
kubectl -n oil-production get networkpolicies
NAME                        POD-SELECTOR   AGE
capsule-oil-0               <none>         42h
production-network-policy   <none>         3m
```

And delete the namespace network policies

```
kubectl -n oil-production delete networkpolicy production-network-policy
```

Any attempt of Alice to delete the tenant network policy defined in the tenant manifest is denied by the Validation Webhook enforcing it.

# What’s next
See how Bill can enforce the Pod containers image pull policy to `Always` to avoid leaking of private images when running on shared nodes. [Enforcing Pod containers image PullPolicy](/docs/operator/use-cases/images-pullpolicy)
