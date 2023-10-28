# Development

Our Makefile helps you with the development of new changes or fixes. [You may have a look at it](./Makefile), since not all targets are documented.

To execute your changes locally, you can run the binary locally. This will run just the capsule controller. We recommend [to setup a development environment](#development-environment) for a better development experience:

```bash
make run
```
## Building

You can build the docker image locally, Ko will be installed via go, so you don't need to install it manually.

```bash
make ko-build-all
```

This will push the build to your local docker images.

## Test

Execute unit testing:

```bash
make test
```

## E2E Test

**New changes always require dedcated E2E tests. E2E help us to ensure the quality of the code and it's functionality.**

For E2E test we use the [ginkgo](https://github.com/onsi/ginkgo) framework. Ou can see all the test under [e2e](./e2e/).


With the following command a new KinD cluster is created with the Kubernetes version `v1.20.7` (This can be done with any available Kubernetes version). A docker image is created and pushed and loaded into the KinD cluster. Then the E2E tests are executed against the KinD cluster.

```bash
make e2e/v1.20.7
```

You can also just run the e2e tests without the creation of a new kind cluster:

```
make e2e-exec
```

The E2E tests are also executed via the [github workflow](./.github/workflows/e2e.yaml) on every PR and push to the main branch.

# Development Environment

During development, we prefer that the code is running within our IDE locally, instead of running as the normal Pod(s) within the Kubernetes cluster.

Such a setup can be illustrated as below diagram:

![Development Environment](./assets/docs/dev-env.png)

## Setup Development Environment

To achieve that, there are some necessary steps we need to walk through, which have been made as a make target within our Makefile.

So the TL;DR answer is:

**Make sure a *KinD* cluster is running on your laptop, and then run `make dev-setup` to setup the dev environment.**. This is not done in the `make dev-setup` setup. 

```bash
# If you haven't installed or run `make deploy` before, do it first
# Note: please retry if you saw errors
$ make deploy

# To retrieve your laptop's IP and execute `make dev-setup` to setup dev env
# For example: LAPTOP_HOST_IP=192.168.10.101 make dev-setup
$ LAPTOP_HOST_IP="<YOUR_LAPTOP_IP>" make dev-setup
```

### Explenation

We recommend to setup the development environment with the make `dev-setup` target. However here is a step by step guide to setup the development environment for understanding.

1. Scaling down the deployed Pod(s) to 0
We need to scale the existing replicas of capsule-controller-manager to 0 to avoid reconciliation competition between the Pod(s) and the code running outside of the cluster, in our preferred IDE for example.

```bash
$ kubectl -n capsule-system scale deployment capsule-controller-manager --replicas=0
deployment.apps/capsule-controller-manager scaled
```

2. Preparing TLS certificate for the webhooks
Running webhooks requires TLS, we can prepare the TLS key pair in our development env to handle HTTPS requests.

```bash
# Prepare a simple OpenSSL config file
# Do remember to export LAPTOP_HOST_IP before running this command
$ cat > _tls.cnf <<EOF
[ req ]
default_bits       = 4096
distinguished_name = req_distinguished_name
req_extensions     = req_ext
[ req_distinguished_name ]
countryName                = SG
stateOrProvinceName        = SG
localityName               = SG
organizationName           = CAPSULE
commonName                 = CAPSULE
[ req_ext ]
subjectAltName = @alt_names
[alt_names]
IP.1   = ${LAPTOP_HOST_IP}
EOF

# Create this dir to mimic the Pod mount point
$ mkdir -p /tmp/k8s-webhook-server/serving-certs

# Generate the TLS cert/key under /tmp/k8s-webhook-server/serving-certs
$ openssl req -newkey rsa:4096 -days 3650 -nodes -x509 \
  -subj "/C=SG/ST=SG/L=SG/O=CAPSULE/CN=CAPSULE" \
  -extensions req_ext \
  -config _tls.cnf \
  -keyout /tmp/k8s-webhook-server/serving-certs/tls.key \
  -out /tmp/k8s-webhook-server/serving-certs/tls.crt

# Clean it up
$ rm -f _tls.cnf
```

3. Patching the Webhooks
By default, the webhooks will be registered with the services, which will route to the Pods, inside the cluster. We need to delegate the controllers' and webhook's services to the code running in our IDE by patching the `MutatingWebhookConfiguration` and `ValidatingWebhookConfiguration`.

```bash
# Export your laptop's IP with the 9443 port exposed by controllers/webhooks' services
$ export WEBHOOK_URL="https://${LAPTOP_HOST_IP}:9443"

# Export the cert we just generated as the CA bundle for webhook TLS
$ export CA_BUNDLE=`openssl base64 -in /tmp/k8s-webhook-server/serving-certs/tls.crt | tr -d '\n'`

kubectl patch MutatingWebhookConfiguration capsule-mutating-webhook-configuration \
	--type='json' -p="[\
		{'op': 'replace', 'path': '/webhooks/0/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/defaults\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/1/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/defaults\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/2/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/defaults\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/3/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/namespace-owner-reference\",'caBundle':\"$${CA_BUNDLE}\"}}\
    ]"

kubectl patch ValidatingWebhookConfiguration capsule-validating-webhook-configuration \
    --type='json' -p="[\
		{'op': 'replace', 'path': '/webhooks/0/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/cordoning\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/1/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/ingresses\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/2/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/namespaces\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/3/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/networkpolicies\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/4/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/nodes\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/5/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/pods\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/6/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/persistentvolumeclaims\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/7/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/services\",'caBundle':\"$${CA_BUNDLE}\"}},\
		{'op': 'replace', 'path': '/webhooks/8/clientConfig', 'value':{'url':\"$${WEBHOOK_URL}/tenants\",'caBundle':\"$${CA_BUNDLE}\"}}\
	]"

kubectl patch crd tenants.capsule.clastix.io \
	--type='json' -p="[\
		{'op': 'replace', 'path': '/spec/conversion/webhook/clientConfig', 'value':{'url': \"$${WEBHOOK_URL}\", 'caBundle': \"$${CA_BUNDLE}\"}}\
	]"

kubectl patch crd capsuleconfigurations.capsule.clastix.io \
	--type='json' -p="[\
		{'op': 'replace', 'path': '/spec/conversion/webhook/clientConfig', 'value':{'url': \"$${WEBHOOK_URL}\", 'caBundle': \"$${CA_BUNDLE}\"}}\
	]";
```

## Running Capsule

When the Development Environment is set up, we can run Capsule controllers with webhooks outside of the Kubernetes cluster:

```bash
$ export NAMESPACE=capsule-system && export TMPDIR=/tmp/
$ go run .
```

To verify that, we can open a new console and create a new Tenant in a new shell:

```bash
$ kubectl apply -f - <<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - name: alice
    kind: User
EOF
```

We should see output and logs in the make run console.

Now it's time to work through our familiar inner loop for development in our preferred IDE. For example, if you're using [Visual Studio Code](https://code.visualstudio.com/), this launch.json file can be a good start.


## Helm Chart

You can test your changes made to the helm chart locally. They are almost identical to the checks executed in the github workflows.

Run chart linting (ct lint):

```bash
make helm-lint
```

Run chart tests (ct install). This creates a KinD cluster, builds the current image and loads it into the cluster and installs the helm chart:

```bash
make helm-test
```

### Documentation

Documentation of the chart is done with [helm-docs](https://github.com/norwoodj/helm-docs). Therefor all documentation relevant changes for the chart must be done in the [README.md.gotmpl](./charts/capsule/README.md.gotmpl) file. You can run this locally with this command (requires running docker daemon):

```bash
make helm-docs

...

time="2023-10-23T13:45:08Z" level=info msg="Found Chart directories [charts/capsule]"
time="2023-10-23T13:45:08Z" level=info msg="Generating README Documentation for chart /helm-docs/charts/capsule"
```

This will update the documentation for the chart in the `README.md` file. 

### Helm Changelog 

The `version` of the chart does not require a bump, since it's driven by our release process. The `appVersion` of the chart is the version of the Capsule project. This is the version that should be bumped when a new Capsule version is released. This will be done by the maintainers.

To create the proper changelog for the helm chart, all changes which affect the helm chart must be documented as chart annotation. See all the available [chart annotations](https://artifacthub.io/docs/topics/annotations/helm/).

This annotation can be provided using two different formats: using a plain list of strings with the description of the change or using a list of objects with some extra structured information (see example below). Please feel free to use the one that better suits your needs. The UI experience will be slightly different depending on the choice. When using the list of objects option the valid supported kinds are `added`, `changed`, `deprecated`, `removed`, `fixed` and `security`.