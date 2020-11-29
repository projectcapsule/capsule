# Set network policies in the tenant
Kubernetes network policies allow controlling network traffic between namespaces and between pods in the same namespace. Bill, the cluster admin, can enforce network traffic isolation between different tenants while leaving to Alice, the tenant owner, the freedom to set isolation between namespaces in the same tenant or even between pods in the same namespace.

To meet this requirement, Bill needs to define network policies that deny pods belonging to a tenant namespace to access pods in namespaces belonging to other tenants or in system namespaces, (e.g. `kube-system`).

Also, Bill can make sure pods belonging to a tenant namespace cannot access other network infrastructure like cluster nodes, load balancers, and virtual machines running other services.  

Bill can set network policies in the tenant manifest, according to the requirements:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  networkPolicies:
    - policyTypes:
      - Ingress
      - Egress
      podSelector: {}
      ingress:
      - from:
        - namespaceSelector: {}
        - podSelector: {}
        - ipBlock:
            cidr: 192.168.0.0/16
      egress:
      - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
            - 192.168.0.0/16
```

The Capsule controller, watching for Namespace creation, creates the Network Policies for each Namespace in the tenant.

Alice has access to these network policies:

```
alice@caas# kubectl -n oil-production get networkpolicies
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         42h


alice@caas# kubectl -n oil-production describe networkpolicy
Name:         capsule-oil-0
Namespace:    oil-production
Created on:   2020-07-20 20:40:28 +0200 CEST
Labels:       capsule.clastix.io/network-policy=0
              capsule.clastix.io/tenant=oil
Annotations:  <none>
Spec:
  PodSelector:     <none> (Allowing the specific traffic to all pods in this namespace)
  Allowing ingress traffic:
    To Port: <any> (traffic allowed to all ports)
    From:
      NamespaceSelector: <none>
    From:
      PodSelector: <none>
    From:
      IPBlock:
        CIDR: 192.168.0.0/12
        Except: 
  Allowing egress traffic:
    To Port: <any> (traffic allowed to all ports)
    To:
      IPBlock:
        CIDR: 0.0.0.0/0
        Except: 192.168.0.0/12
  Policy Types: Ingress, Egress  
```

Alice can create, patch, and delete additional network policies within her namespaces

```
alice@caas# kubectl -n oil-production auth can-i get networkpolicies
yes

alice@caas# kubectl -n oil-production auth can-i delete networkpolicies
yes

alice@caas# kubectl -n oil-production auth can-i patch networkpolicies
yes
```

For example, she can create

```yaml
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
```

Check all the network policies

```
alice@caas# kubectl -n oil-production get networkpolicies
NAME                        POD-SELECTOR   AGE
capsule-oil-0               <none>         42h
production-network-policy   <none>         3m
```

an delete the namespace network-policies

```
alice@caas# kubectl -n oil-production delete networkpolicy production-network-policy
```


However, the Capsule controller prevents Alice to delete the tenant network policy:

```
alice@caas# kubectl -n oil-production delete networkpolicy capsule-oil-0
Error from server (Capsule Network Policies cannot be deleted: please, reach out the system administrators): admission webhook "validating.network-policy.capsule.clastix.io" denied the request: Capsule Network Policies cannot be deleted: please, reach out the system administrators
```

# What’s next
See how Bill, the cluster admin, can assign trusted images registries to Alice's tenant. [Assign Trusted Images Registries to a tenant]().