# Assign Ingress Classes to a tenant
An Ingress Controller is used in Kubernetes to publish services and applications outside of the cluster. An Ingress Controller can be provisioned to accept only Ingresses with a given Ingress Class.

Bill can assign a set of dedicated Ingress Classes to the `oil` tenant to force the applications in the `oil` tenant to be published only by the assigned Ingress Controller: 

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
  ingressClasses:
     allowed:
     - oil
  ...
```

It is also possible to use regular expression for assigning Ingress Classes:

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
  ingressClasses:
     allowedRegex: "^oil-.*$"
  ...
```

The Capsule controller assures that all Ingresses created in the tenant can use only one of the valid Ingress Classes. This is achieved by checking the annotation `kubernetes.io/ingress.class:` in the Ingress.

Alice, as tenant owner, gets the list of valid Ingress Classes by checking any of her namespaces:

```
alice@caas# kubectl describe ns oil-production
Name:         oil-production
Labels:       capsule.clastix.io/tenant=oil
Annotations:  capsule.clastix.io/ingress-classes: oil
              capsule.clastix.io/ingress-classes-regexp: ^oil-.*$
...
```

Alice creates an Ingress using a valid Ingress Class in the annotation:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: oil
spec:
  rules:
  - host: web.oil-inc.com
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
        path: /
```

Any tentative of Alice to use a not valid Ingress Class, e.g. `default`, will fail:

```
Error from server: error when creating nginx": admission webhook "extensions.ingress.capsule.clastix.io" denied the request: Ingress Class default is forbidden for the current Tenant
```

The effect of this policy is that the services created in the tenant will be published only on the Ingress Controller designated by Bill to accept only one of the permitted Ingress Classes.

# What’s next
See how Bill, the cluster admin, can assign a Storage Class to Alice's tenant. [Assign Storage Classes to a tenant]().