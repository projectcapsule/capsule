# Sidecar Installation
The `capsule-proxy` can be deployed as sidecar container for server-side Kubernetes dashboards. It will intercept all requests sent from the client side to the server-side of the dashboard and it will proxy them to the Kubernetes APIs server.

```
                                      capsule-proxy
                                      +------------+        +------------+
                                      |:9001       +------->|:6443       |
                                      +------------+        +------------+
                +-----------+         |            |        kube-apiserver
browser +------>+:443       +-------->+:8443       |
                +-----------+         +------------+ 
                ingress-controller    dashboard backend            
                (ssl-passthrough)     
```

In order to use this pattern, the server-side backend of your dashboard must permit to specify the URL of the Kubernetes APIs server. For example, the following manifest contains an excerpt for deploying with [Kubernetes Dashboard](https://github.com/kubernetes/dashboard), and the Ingress Controller in ssl-passthrough mode.

Place the `capsule-proxy` in a pod with SSL mode, i.e. `--enable-ssl=true` and passing valid certificate and key files in a secret.

```yaml
...
  template:
    metadata:
      labels:
        k8s-app: kubernetes-dashboard
    spec:
      containers:
        - name: ns-filter
          image: quay.io/clastix/capsule-proxy
          imagePullPolicy: IfNotPresent 
          args:
          - --capsule-user-group=capsule.clastix.io
          - --zap-log-level=5
          - --enable-ssl=true
          - --ssl-cert-path=/opt/certs/tls.crt
          - --ssl-key-path=/opt/certs/tls.key
          volumeMounts:
            - name: ns-filter-certs
              mountPath: /opt/certs
          ports:
          - containerPort: 9001
            name: http
            protocol: TCP
...
```

In the same pod, place the Kubernetes Dashboard in _"out-of-cluster"_ mode with `--apiserver-host=https://localhost:9001` to send all the requests to the `capsule-proxy` sidecar container:


```yaml
...
        - name: dashboard
          image: kubernetesui/dashboard:v2.0.4
          imagePullPolicy: Always
          ports:
            - containerPort: 8443
              protocol: TCP
          args:
            - --auto-generate-certificates
            - --namespace=cmp-system
            - --tls-cert-file=tls.crt
            - --tls-key-file=tls.key
            - --apiserver-host=https://localhost:9001
            - --kubeconfig=/opt/.kube/config
          volumeMounts:
            - name: kubernetes-dashboard-certs
              mountPath: /certs
            - mountPath: /tmp
              name: tmp-volume
            - mountPath: /opt/.kube
              name: kubeconfig
          livenessProbe:
            httpGet:
              scheme: HTTPS
              path: /
              port: 8443
            initialDelaySeconds: 30
            timeoutSeconds: 30
...
```

Make sure you pass a valid `kubeconfig` file to the dashboard pointing to the `capsule-proxy` sidecar container instead of the `kube-apiserver` directly:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kubernetes-dashboard-kubeconfig
  namespace: kubernetes-dashboard
data:
  config: |
    kind: Config
    apiVersion: v1
    clusters:
    - cluster:
        insecure-skip-tls-verify: true
        server: https://localhost:9001  # <- point to the capsule-proxy
      name: localhost
    contexts:
    - context:
        cluster: localhost
        user: kubernetes-admin          # <- dashboard has cluster-admin permissions
      name: admin@localhost
    current-context: admin@localhost
    preferences: {}
    users:
    - name: kubernetes-admin
      user:
        client-certificate-data: REDACTED
        client-key-data: REDACTED
```

After starting the dashboard, login as a Tenant Owner user, e.g. `alice` according to the used authentication method, and check you can see only owned namespaces.

The `capsule-proxy` can be deployed in standalone mode, in order to be used with a command line tools like `kubectl`. See [Standalone Installation](/docs/proxy/standalone).

