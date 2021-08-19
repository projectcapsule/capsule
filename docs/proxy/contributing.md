# How to contribute to Capsule Proxy
First, thanks for your interest in Capsule and Capsule Proxy, any contribution is welcome!

You should setup your development environment as following:

- [Go 1.16](https://golang.org/dl/)
- [KinD](https://github.com/kubernetes-sigs/kind)

> Please, refer to the general coding style rules for Capsule. 

### Run locally for test and debug

This guide helps new contributors to locally debug in _out or cluster_ mode the project.

1. You need to run a Kind cluster and find the endpoint port of `kind-control-plane` using `docker ps`:

```bash
❯ docker ps
CONTAINER ID   IMAGE                  COMMAND                  CREATED          STATUS          PORTS                       NAMES
88432e392adb   kindest/node:v1.20.2   "/usr/local/bin/entr…"   32 seconds ago   Up 28 seconds   127.0.0.1:64582->6443/tcp   kind-control-plane
```

2. You need to generate TLS cert keys for localhost, you can use [mkcert](https://github.com/FiloSottile/mkcert):

```bash
> cd /tmp
> mkcert localhost
> ls
localhost-key.pem localhost.pem
```

3. Run the proxy with the following options

```bash
go run main.go --ssl-cert-path=/tmp/localhost.pem --ssl-key-path=/tmp/localhost-key.pem --enable-ssl=true --kubeconfig=<YOUR KUBERNETES CONFIGURATION FILE>
```

5. Edit the `KUBECONFIG` file (you should make a copy and work on it) as follows:
- Find the section of your cluster
- replace the server path with `https://127.0.0.1:9001`
- replace the certificate-authority-data path with the content of your rootCA.pem file. (if you use mkcert, you'll find with `cat "$(mkcert -CAROOT)/rootCA.pem"|base64|tr -d '\n'`)

6. Now you should be able to run kubectl using the proxy!

### Debug in a remote Kubernetes cluster

In some cases, you would need to debug the in-cluster mode and [`delve`](https://github.com/go-delve/delve) plays a big role here.

1. build the Docker image with `delve` issuing `make dlv-build`
2. with the `quay.io/clastix/capsule-proxy:dlv` produced Docker image, publish it or load it to your [KinD](https://github.com/kubernetes-sigs/kind) instance (`kind load docker-image --name capsule --nodes capsule-control-plane quay.io/clastix/capsule-proxy:dlv`)
3. change the Deployment image using `kubectl edit` or `kubectl set image deployment/capsule-proxy capsule-proxy=quay.io/clastix/capsule-proxy:dlv`
4. wait for the image rollout (`kubectl -n capsule-system rollout status deployment/capsule-proxy`)
5. perform the port-forwarding with `kubectl -n capsule-system port-forward $(kubectl -n capsule-system get pods -l app.kubernetes.io/name=capsule-proxy --output name) 2345:2345`
6. connect using your `delve` options

> _Nota Bene_: the application could be killed by the Liveness Probe since delve will wait for the debugger connection before starting it.
> Feel free to edit and remove the probes to avoid this kind of issue.