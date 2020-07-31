# How to contribute to Capsule

First, thanks for your interest on Capsule, any contribution is welcome!

The first step is to setup your local development environment

## Setting up the development environment

The following dependencies are mandatory:

- [Go 1.13.8](https://golang.org/dl/)
- [OperatorSDK 1.8](https://github.com/operator-framework/operator-sdk)
- [KinD](https://github.com/kubernetes-sigs/kind)
- [ngrok](https://ngrok.com/) (if you want to run locally)
- [golangci-lint](https://github.com/golangci/golangci-lint)

### Installing Go dependencies

After cloning Capsule on any folder, access it and issue the following command
to ensure all dependencies are properly download.

```
go mod download
```

### Installing Operator SDK

Some operations, like the Docker image build process or the code-generation of
the CRDs manifests, as well the deep copy functions, require _Operator SDK_:
the binary has to be installed into your `PATH`.

### Installing KinD

Capsule is able to run on any certified Kubernetes installation and locally
the whole development is performed on _KinD_, also knows as
[Kubernetes in Docker](https://github.com/kubernetes-sigs/kind).

> N.B.: Docker is hard requirement since it's based on it

According to your operative system and architecture, download the right binary
and place it on your `PATH`.

Once done, you're ready to bootstrap in a glance of seconds, a fully functional
Kubernetes cluster.

```
# kind create cluster --name capsule
Creating cluster "capsule" ...
 ✓ Ensuring node image (kindest/node:v1.18.2) 🖼
 ✓ Preparing nodes 📦  
 ✓ Writing configuration 📜 
 ✓ Starting control-plane 🕹️ 
 ✓ Installing CNI 🔌 
 ✓ Installing StorageClass 💾 
Set kubectl context to "kind-capsule"
You can now use your cluster with:

kubectl cluster-info --context kind-capsule

Thanks for using kind! 😊
```

The current `KUBECONFIG` will be populated with the `cluster-admin`
certificates and the context changed to the just born Kubernetes cluster.

### Build the Docker image and push it to KinD

From the root path, issue the _make_ recipe:

```
# make docker-image
operator-sdk build quay.io/clastix/capsule:latest
INFO[0001] Building OCI image quay.io/clastix/capsule:latest 
Sending build context to Docker daemon  89.26MB
Step 1/7 : FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
 ---> 75a64ccf990b
Step 2/7 : ENV OPERATOR=/usr/local/bin/capsule     USER_UID=0     USER_NAME=capsule
 ---> Using cache
 ---> e4610bd8596f
Step 3/7 : COPY build/_output/bin/capsule ${OPERATOR}
 ---> Using cache
 ---> 1f6196485c28
Step 4/7 : COPY build/bin /usr/local/bin
 ---> Using cache
 ---> b517a62ca352
Step 5/7 : RUN  /usr/local/bin/user_setup
 ---> Using cache
 ---> e879394010d5
Step 6/7 : ENTRYPOINT ["/usr/local/bin/entrypoint"]
 ---> Using cache
 ---> 6e740290e0e4
Step 7/7 : USER ${USER_UID}
 ---> Using cache
 ---> ebb8f640dda1
Successfully built ebb8f640dda1
Successfully tagged quay.io/clastix/capsule:latest
INFO[0004] Operator build complete. 
```

The image `quay.io/clastix/capsule:latest` will be available locally, you just
need to push it to kind with the following command.

```
# kind load docker-image --nodes capsule-control-plane --name capsule quay.io/clastix/capsule:latest
Image: "quay.io/clastix/capsule:latest" with ID "sha256:ebb8f640dda129a795ddc68bad125cb50af6bfb8803be210b56314ded6355759" not yet present on node "capsule-control-plane", loading...
```

### Deploy the Kubernetes manifests

With the current `kind-capsule` context enabled, create the `capsule-system`
Namespace that will contain all the Kubernetes resources.

```
# kubectl create namespace capsule-system
namespace/capsule-system created
```

Now it's time to install the _Custom Resource Definition_:

```
# kubectl apply -f deploy/crds/capsule.clastix.io_tenants_crd.yaml
customresourcedefinition.apiextensions.k8s.io/tenants.capsule.clastix.io created
```

Finally, install the required manifests issuing the following command:

```
# kubectl apply -f deploy
mutatingwebhookconfiguration.admissionregistration.k8s.io/capsule created
clusterrole.rbac.authorization.k8s.io/namespace:deleter created
clusterrole.rbac.authorization.k8s.io/namespace:provisioner created
clusterrolebinding.rbac.authorization.k8s.io/namespace:provisioner created
deployment.apps/capsule created
clusterrole.rbac.authorization.k8s.io/capsule created
clusterrolebinding.rbac.authorization.k8s.io/capsule-cluster-admin created
clusterrolebinding.rbac.authorization.k8s.io/capsule created
secret/capsule-ca created
secret/capsule-tls created
service/capsule created
serviceaccount/capsule created
```

You can check if Capsule is running checking the logs:

```
# kubectl -n capsule-system logs -f -l name=capsule
...
{"level":"info","ts":1596125071.5951712,"logger":"controller-runtime.controller","msg":"Starting workers","controller":"tenant-controller","worker count":1}
```

Since Capsule is built using _OperatorSDK_, logging is handled by the zap
module: verbosity increase can be controlled using the CLI flag `--zap-level`
with a value from `1` to `10` or the [basic keywords](https://godoc.org/go.uber.org/zap/zapcore#Level).

> CA generation
>
> You could notice a restart of the Capsule pod upon installation, that's ok:
> Capsule is generating the CA and populating the Secret containing the TLS
> certificate to handle the webhooks and there's the need the reload the whole
> application to serve properly HTTPS requests.

### Run Capsule locally

Debugging remote applications is always struggling but Operators just need
access to the Kubernetes API Server.

#### Scaling down the remote Pod

First, ensure the Capsule pod is not running scaling down the Deployment.

```
# kubectl -n capsule-system scale deployment capsule --replicas=0
deployment.apps/capsule scaled
```

> This is mandatory since Capsule uses Leader Election

#### Providing TLS certificate for webhooks

Next step is to replicate the same environment Capsule is expecting in the Pod,
it means creating a fake certificate to handle HTTP requests.

``` bash
mkdir -p /tmp/k8s-webhook-server/serving-certs
kubectl -n capsule-system get secret capsule-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > /tmp/k8s-webhook-server/serving-certs/tls.crt
kubectl -n capsule-system get secret capsule-tls -o jsonpath='{.data.tls\.key}' | base64 -d > /tmp/k8s-webhook-server/serving-certs/tls.key
```

> We're using the certificates generate upon first installation of Capsule:
> it means the Secret will be populated at first start-up.
> If you plan to run it locally since the beginning, it means you will require
> to provide a self-signed certificate in the said directory.

#### Starting NGROK

In another session we need a `ngrok` session, mandatory to debug also webhooks
(YMMV).

```
# ngrok http localhost:443
ngrok by @inconshreveable

Session Status                online
Account                       Dario Tranchitella (Plan: Free)
Version                       2.3.35
Region                        United States (us)
Web Interface                 http://127.0.01:4040
Forwarding                    http://cdb72b99348c.ngrok.io -> https://localhost:443
Forwarding                    https://cdb72b99348c.ngrok.io -> https://localhost:443
Connections                   ttl     opn     rt1     rt5     p50     p90 
                              0       0       0.00    0.00    0.00    0.00
```

What we need is the _ngrok_ URL (in this case, `https://cdb72b99348c.ngrok.io`)
since we're going to use this default URL as the `url` parameter for the
_Dynamic Admissions Control Webhooks_.

#### Patching the MutatingWebhookConfiguration

Now it's time to patch the _MutatingWebhookConfiguration_, adding the said
`ngrok` URL as base for each defined webhook, as following:

```diff
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: capsule
webhooks:
  - name: owner.namespace.capsule.clastix.io
    failurePolicy: Fail
    rules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE"]
        resources: ["namespaces"]
    clientConfig:
+     url: https://cdb72b99348c.ngrok.io/mutate-v1-namespace-owner-reference
-     caBundle:
-     service:
-       namespace: capsule-system
-       name: capsule
-       path: /mutate-v1-namespace-owner-reference
...
```

#### Run Capsule

Finally, it's time to run locally Capsule using your preferred IDE (or not):
from the project root path you can issue the following command.

```
WATCH_NAMESPACE= KUBECONFIG=/path/to/your/kubeconfig OPERATOR_NAME=capsule go run cmd/manager/main.go
```

All the logs will start to flow in your standard output, feel free to attach
your debugger to set breakpoints as well!

## Code convention

The changes must follow the Pull Request method where a _GitHub Action_ will
check the `golangci-lint`, so ensure your changes respect the coding standard.

### golint

You can easily check them issuing the _Make_ recipe `golint`.

```
# make golint
golangci-lint run
```

### goimports

Also the Go import statements must be sorted following the best practice:

```
<STANDARD LIBRARY>

<EXTERNAL PACKAGES>

<LOCAL PACKAGES>
```

To help you out you can use the _Make_ recipe `goimports`

```
# make goimports
goimports -w -l -local "github.com/clastix/capsule" .
```

### Commits

All the Pull Requests must reference to an already open issue: this is the
first phase to contribute also for informing maintainers about the issue.

Commit first line should not exceed 50 columns.

A commit description is welcomed to explain more the changes: just ensure
to put a blank line and an arbitrary number of maximum 72 characters long
lines, at most one blank line between them.

Please, split changes into several and documented small commits: this will help
us to perform a better review.

> In case of errors or need of changes to previous commits,
> fix them squashing in order to make changes atomic.
