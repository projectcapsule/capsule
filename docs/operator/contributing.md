# How to contribute to Capsule
First, thanks for your interest in Capsule, any contribution is welcome!

The first step is to set up your local development environment as stated below:

## Setting up the development environment
The following dependencies are mandatory:

- [Go 1.16](https://golang.org/dl/)
- [OperatorSDK 1.7.2](https://github.com/operator-framework/operator-sdk)
- [Kubebuilder](https://github.com/kubernetes-sigs/kubebuilder)
- [KinD](https://github.com/kubernetes-sigs/kind)
- [ngrok](https://ngrok.com/) (if you want to run locally)
- [golangci-lint](https://github.com/golangci/golangci-lint)

### Installing Go dependencies
After cloning Capsule on any folder, access it and issue the following command
to ensure all dependencies are properly downloaded.

```
go mod download
```

### Installing Operator SDK
Some operations, like the Docker image build process or the code-generation of
the CRDs manifests, as well the deep copy functions, require _Operator SDK_:
the binary has to be installed into your `PATH`.

### Installing Kubebuilder
With the latest release of OperatorSDK there's a more tightly integration with
Kubebuilder and its opinionated testing suite: ensure to download the latest
binaries available from the _Releases_ GitHub page and place them into the
`/usr/local/kubebuilder/bin` folder, ensuring this is also in your `PATH`.

### Installing KinD
Capsule can run on any certified Kubernetes installation and locally
the whole development is performed on _KinD_, also knows as
[Kubernetes in Docker](https://github.com/kubernetes-sigs/kind).

> N.B.: Docker is a hard requirement since it's based on it

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
# make docker-build
```

The image `quay.io/clastix/capsule:<tag>` will be available locally. Built image `<tag>` is resulting last one available [release](https://github.com/clastix/capsule/releases).

Push it to _KinD_ with the following command:

```
# kind load docker-image --nodes capsule-control-plane --name capsule quay.io/clastix/capsule:<tag>
```

### Deploy the Kubernetes manifests
With the current `kind-capsule` context enabled, deploy all the required
manifests issuing the following command:

```
make deploy
```

This will install all the required Kubernetes resources, automatically.

You can check if Capsule is running tailing the logs:

```
# kubectl -n capsule-system logs --all-containers -f -l control-plane=controller-manager
```

Since Capsule is built using _OperatorSDK_, logging is handled by the zap
module: log verbosity of the Capsule controller can be increased by passing
the `--zap-log-level` option with a value from `1` to `10` or the
[basic keywords](https://godoc.org/go.uber.org/zap/zapcore#Level) although
it is suggested to use the `--zap-devel` flag to get also stack traces.

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
# kubectl -n capsule-system scale deployment capsule-controller-manager --replicas=0
deployment.apps/capsule-controller-manager scaled
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
In another session, we need a `ngrok` session, mandatory to debug also webhooks
(YMMV).

```
# ngrok http https://localhost:9443
ngrok by @inconshreveable

Session Status                online
Account                       Dario Tranchitella (Plan: Free)
Version                       2.3.35
Region                        United States (us)
Web Interface                 http://127.0.01:4040
Forwarding                    http://cdb72b99348c.ngrok.io -> https://localhost:9443
Forwarding                    https://cdb72b99348c.ngrok.io -> https://localhost:9443
Connections                   ttl     opn     rt1     rt5     p50     p90 
                              0       0       0.00    0.00    0.00    0.00
```

What we need is the _ngrok_ URL (in this case, `https://cdb72b99348c.ngrok.io`)
since we're going to use this default URL as the `url` parameter for the
_Dynamic Admissions Control Webhooks_.

#### Patching the MutatingWebhookConfiguration
Now it's time to patch the _MutatingWebhookConfiguration_ and the
_ValidatingWebhookConfiguration_ too, adding the said `ngrok` URL as base for
each defined webhook, as following:

```diff
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: capsule-mutating-webhook-configuration
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
-       namespace: system
-       name: capsule
-       path: /mutate-v1-namespace-owner-reference
...
```

#### Run Capsule
Finally, it's time to run locally Capsule using your preferred IDE (or not):
from the project root path, you can issue the following command.

```
make run
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
golangci-lint run -c .golangci.yml
```

> Enabled linters and related options are defined in the [.golanci.yml file](../../.golangci.yml)

### goimports
Also, the Go import statements must be sorted following the best practice:

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
All the Pull Requests must refer to an already open issue: this is the first phase to contribute also for informing maintainers about the issue.

Commit's first line should not exceed 50 columns.

A commit description is welcomed to explain more the changes: just ensure
to put a blank line and an arbitrary number of maximum 72 characters long
lines, at most one blank line between them.

Please, split changes into several and documented small commits: this will help
us to perform a better review.

> In case of errors or need of changes to previous commits,
> fix them squashing to make changes atomic.
