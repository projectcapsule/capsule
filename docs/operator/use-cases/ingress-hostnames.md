# Assign Ingress Hostnames
Bill can assign a set of dedicated ingress hostnames to the `oil` tenant in order to force the applications in the tenant to be published only using the given hostnames: 

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  ingressHostnames:
     allowed:
     - *.oil.acmecorp.com
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
  ingressHostnames:
     allowedRegex: "^oil-acmecorp.*$"
  ...
```

The Capsule controller assures that all Ingresses created in the tenant can use only one of the valid hostnames. 

Alice creates an Ingress using an allowed hostname

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: oil
spec:
  rules:
  - host: web.oil.acmecorp.com
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
        path: /
```

Any tentative of Alice to use a not valid hostname, e.g. `web.gas.acmecorp.org`, will fail.

# What’s next
See how Bill, the cluster admin, can assign a Storage Class to Alice's tenant. [Assign Storage Classes](./storage-classes.md).