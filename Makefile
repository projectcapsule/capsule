# Version
GIT_HEAD_COMMIT ?= $(shell git rev-parse --short HEAD)
VERSION         ?= $(or $(shell git describe --abbrev=0 --tags --match "v*" 2>/dev/null),$(GIT_HEAD_COMMIT))
GOOS                 ?= $(shell go env GOOS)
GOARCH               ?= $(shell go env GOARCH)

# Defaults
REGISTRY        ?= ghcr.io
REPOSITORY      ?= projectcapsule/capsule
GIT_TAG_COMMIT  ?= $(shell git rev-parse --short $(VERSION))
GIT_MODIFIED_1  ?= $(shell git diff $(GIT_HEAD_COMMIT) $(GIT_TAG_COMMIT) --quiet && echo "" || echo ".dev")
GIT_MODIFIED_2  ?= $(shell git diff --quiet && echo "" || echo ".dirty")
GIT_MODIFIED    ?= $(shell echo "$(GIT_MODIFIED_1)$(GIT_MODIFIED_2)")
GIT_REPO        ?= $(shell git config --get remote.origin.url)
BUILD_DATE      ?= $(shell git log -1 --format="%at" | xargs -I{} sh -c 'if [ "$(shell uname)" = "Darwin" ]; then date -r {} +%Y-%m-%dT%H:%M:%S; else date -d @{} +%Y-%m-%dT%H:%M:%S; fi')
IMG_BASE        ?= $(REPOSITORY)
IMG             ?= $(IMG_BASE):$(VERSION)
CAPSULE_IMG     ?= $(REGISTRY)/$(IMG_BASE)
CLUSTER_NAME    ?= capsule

## Kubernetes Version Support
KUBERNETES_SUPPORTED_VERSION ?= "v1.31.0"

## Tool Binaries
KUBECTL ?= kubectl
HELM ?= helm

# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager

# Run tests
.PHONY: test
test: test-clean generate manifests test-clean
	@GO111MODULE=on go test -v ./... -coverprofile coverage.out

.PHONY: test-clean
test-clean: ## Clean tests cache
	@go clean -testcache

# Build manager binary
manager: generate golint
	go build -o bin/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate manifests
	go run .

# Generate manifests e.g. CRD, RBAC etc.
manifests: generate
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=charts/capsule/crds

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Helm
SRC_ROOT = $(shell git rev-parse --show-toplevel)

helm-controller-version:
	$(eval VERSION := $(shell grep 'appVersion:' charts/capsule/Chart.yaml | awk '{print $$2}'))
	$(eval KO_TAGS := $(shell grep 'appVersion:' charts/capsule/Chart.yaml | awk '{print $$2}'))

helm-docs: helm-doc
	$(HELM_DOCS) --chart-search-root ./charts

helm-lint: ct
	@$(CT) lint --config .github/configs/ct.yaml --validate-yaml=false --all --debug

helm-schema: helm-plugin-schema
	cd charts/capsule && $(HELM) schema -output values.schema.json

helm-test: HELM_KIND_CONFIG ?= ""
helm-test: kind
	@mkdir -p /tmp/results || true
	@$(KIND) create cluster --wait=60s --name capsule-charts --image kindest/node:$(KUBERNETES_SUPPORTED_VERSION) --config $(HELM_KIND_CONFIG)
	@make helm-test-exec
	@$(KIND) delete cluster --name capsule-charts

helm-test-exec: ct helm-controller-version ko-build-all
	$(MAKE) docker-build-capsule-trace
	$(MAKE) e2e-load-image CLUSTER_NAME=capsule-charts IMAGE=$(CAPSULE_IMG) VERSION=v0.0.0
	$(MAKE) e2e-load-image CLUSTER_NAME=capsule-charts IMAGE=$(CAPSULE_IMG) VERSION=tracing
	@$(KUBECTL) create ns capsule-system || true
	@$(KUBECTL) apply --force-conflicts --server-side=true -f https://github.com/cert-manager/cert-manager/releases/download/v1.9.1/cert-manager.crds.yaml
	@$(KUBECTL) apply --force-conflicts --server-side=true -f https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.58.0/bundle.yaml
	@$(CT) install --config $(SRC_ROOT)/.github/configs/ct.yaml --namespace=capsule-system --all --debug

# Setup development env
# Usage:
# 	LAPTOP_HOST_IP=<YOUR_LAPTOP_IP> make dev-setup
# For example:
#	LAPTOP_HOST_IP=192.168.10.101 make dev-setup
define TLS_CNF
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
IP.1   = $(LAPTOP_HOST_IP)
endef
export TLS_CNF
dev-setup:
	mkdir -p /tmp/k8s-webhook-server/serving-certs
	echo "$${TLS_CNF}" > _tls.cnf
	openssl req -newkey rsa:4096 -days 3650 -nodes -x509 \
		-subj "/C=SG/ST=SG/L=SG/O=CAPSULE/CN=CAPSULE" \
		-extensions req_ext \
		-config _tls.cnf \
		-keyout /tmp/k8s-webhook-server/serving-certs/tls.key \
		-out /tmp/k8s-webhook-server/serving-certs/tls.crt
	$(KUBECTL) create secret tls capsule-tls -n capsule-system \
		--cert=/tmp/k8s-webhook-server/serving-certs/tls.crt\
		--key=/tmp/k8s-webhook-server/serving-certs/tls.key || true
	rm -f _tls.cnf
	export WEBHOOK_URL="https://$${LAPTOP_HOST_IP}:9443"; \
	export CA_BUNDLE=`openssl base64 -in /tmp/k8s-webhook-server/serving-certs/tls.crt | tr -d '\n'`; \
	$(HELM) upgrade \
	    --dependency-update \
		--debug \
		--install \
		--namespace capsule-system \
		--create-namespace \
		--set 'crds.install=true' \
		--set 'crds.exclusive=true'\
		--set "webhooks.exclusive=true"\
		--set "webhooks.service.url=$${WEBHOOK_URL}" \
		--set "webhooks.service.caBundle=$${CA_BUNDLE}" \
		capsule \
		./charts/capsule
	$(KUBECTL) -n capsule-system scale deployment capsule-controller-manager --replicas=0 || true

####################
# -- Docker
####################

KO_PLATFORM     ?= linux/$(GOARCH)
KOCACHE         ?= /tmp/ko-cache
KO_REGISTRY     := ko.local
KO_TAGS         ?= "latest"
ifdef VERSION
KO_TAGS         := $(KO_TAGS),$(VERSION)
endif

LD_FLAGS        := "-X main.Version=$(VERSION) \
					-X main.GitCommit=$(GIT_HEAD_COMMIT) \
					-X main.GitTag=$(VERSION) \
					-X main.GitDirty=$(GIT_MODIFIED) \
					-X main.BuildTime=$(BUILD_DATE) \
					-X main.GitRepo=$(GIT_REPO)"

# Docker Image Build
# ------------------

.PHONY: ko-build-capsule
ko-build-capsule: ko
	@echo Building Capsule $(KO_TAGS) for $(KO_PLATFORM) >&2
	@LD_FLAGS=$(LD_FLAGS) KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(CAPSULE_IMG) \
		$(KO) build ./ --bare --tags=$(KO_TAGS) --push=false --local --platform=$(KO_PLATFORM)

.PHONY: ko-build-all
ko-build-all: ko-build-capsule

.PHONY: docker-build-capsule-trace
docker-build-capsule-trace: ko-build-capsule
	@docker build \
		--no-cache \
		--build-arg TARGET_IMAGE=$(CAPSULE_IMG):$(VERSION) \
		-t $(CAPSULE_IMG):tracing \
		-f Dockerfile.tracing .

# Docker Image Publish
# ------------------

REGISTRY_PASSWORD   ?= dummy
REGISTRY_USERNAME   ?= dummy

.PHONY: ko-login
ko-login: ko
	@$(KO) login $(REGISTRY) --username $(REGISTRY_USERNAME) --password $(REGISTRY_PASSWORD)

.PHONY: ko-publish-capsule
ko-publish-capsule: ko-login ## Build and publish kyvernopre image (with ko)
	@LD_FLAGS=$(LD_FLAGS) KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(CAPSULE_IMG) \
		$(KO) build ./ --bare --tags=$(KO_TAGS)

.PHONY: ko-publish-all
ko-publish-all: ko-publish-capsule

# Sorting imports
.PHONY: goimports
goimports:
	goimports -w -l -local "github.com/projectcapsule/capsule" .

# Linting code as PR is expecting
.PHONY: golint
golint: golangci-lint
	$(GOLANGCI_LINT) run -c .golangci.yml --verbose --fix

# Running e2e tests in a KinD instance
.PHONY: e2e
e2e: ginkgo
	$(MAKE) e2e-build && $(MAKE) e2e-exec && $(MAKE) e2e-destroy

e2e-build: kind
	$(KIND) create cluster --wait=60s --name $(CLUSTER_NAME) --image kindest/node:$(KUBERNETES_SUPPORTED_VERSION)
	$(MAKE) e2e-install

.PHONY: e2e-install
e2e-install: ko-build-all
	$(MAKE) e2e-load-image CLUSTER_NAME=$(CLUSTER_NAME) IMAGE=$(CAPSULE_IMG) VERSION=$(VERSION)
	$(HELM) upgrade \
	    --dependency-update \
		--debug \
		--install \
		--namespace capsule-system \
		--create-namespace \
		--set 'manager.image.pullPolicy=Never' \
		--set 'manager.resources=null'\
		--set "manager.image.tag=$(VERSION)" \
		--set 'manager.livenessProbe.failureThreshold=10' \
		--set 'manager.readinessProbe.failureThreshold=10' \
		capsule \
		./charts/capsule

.PHONY: trace-install
trace-install:
	helm upgrade \
	    --dependency-update \
		--debug \
		--install \
		--namespace capsule-system \
		--create-namespace \
		--set 'manager.resources=null'\
		--set 'manager.livenessProbe.failureThreshold=10' \
		--set 'manager.readinessProbe.failureThreshold=10' \
		--values charts/capsule/ci/tracing-values.yaml \
		capsule \
		./charts/capsule

.PHONY: trace-e2e
trace-e2e: kind
	$(MAKE) docker-build-capsule-trace
	$(KIND) create cluster --wait=60s --image kindest/node:$(KUBERNETES_SUPPORTED_VERSION) --config hack/kind-cluster.yml
	$(MAKE) e2e-load-image CLUSTER_NAME=capsule-tracing IMAGE=$(CAPSULE_IMG) VERSION=tracing
	$(MAKE) trace-install
	$(MAKE) e2e-exec
	$(KIND) delete cluster --name capsule-tracing

.PHONY: trace-unit
trace-unit: harpoon
	$(HARPOON) analyze -e .git/ -e assets/ -e charts/ -e config/ -e docs/ -e e2e/ -e hack/ --directory /tmp/artifacts/ --save
	$(HARPOON) hunt -D /tmp/results -F harpoon-report.yml --include-cmd-stdout --save

.PHONY: seccomp
seccomp:
	$(HARPOON) build --add-syscall-sets=dynamic,docker -D /tmp/results --name capsule-seccomp.json --save

.PHONY: e2e-load-image
e2e-load-image: kind
	$(KIND) load docker-image $(IMAGE):$(VERSION) --name $(CLUSTER_NAME)

.PHONY: e2e-exec
e2e-exec: ginkgo
	$(GINKGO) -v -tags e2e ./e2e

.PHONY: e2e-destroy
e2e-destroy: kind
	$(KIND) delete cluster --name capsule

SPELL_CHECKER = npx spellchecker-cli
docs-lint:
	cd docs/content && $(SPELL_CHECKER) -f "*.md" "*/*.md" "!general/crds-apis.md" -d dictionary.txt

####################
# -- Helpers
####################
pull-upstream:
	git remote add upstream https://github.com/capsuleproject/capsule.git
	git fetch --all && git pull upstream

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

####################
# -- Helm Plugins
####################

HELM_SCHEMA_VERSION   := ""
helm-plugin-schema:
	@$(HELM) plugin install https://github.com/losisin/helm-values-schema-json.git --version $(HELM_SCHEMA_VERSION) || true

HELM_DOCS         := $(LOCALBIN)/helm-docs
HELM_DOCS_VERSION := v1.14.1
HELM_DOCS_LOOKUP  := norwoodj/helm-docs
helm-doc:
	@test -s $(HELM_DOCS) || \
	$(call go-install-tool,$(HELM_DOCS),github.com/$(HELM_DOCS_LOOKUP)/cmd/helm-docs@$(HELM_DOCS_VERSION))

####################
# -- Tools
####################
CONTROLLER_GEN         := $(LOCALBIN)/controller-gen
CONTROLLER_GEN_VERSION ?= v0.17.2
CONTROLLER_GEN_LOOKUP  := kubernetes-sigs/controller-tools
controller-gen:
	@test -s $(CONTROLLER_GEN) && $(CONTROLLER_GEN) --version | grep -q $(CONTROLLER_GEN_VERSION) || \
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION))

GINKGO := $(LOCALBIN)/ginkgo
ginkgo:
	$(call go-install-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo)

CT         := $(LOCALBIN)/ct
CT_VERSION := v3.12.0
CT_LOOKUP  := helm/chart-testing
ct:
	@test -s $(CT) && $(CT) version | grep -q $(CT_VERSION) || \
	$(call go-install-tool,$(CT),github.com/$(CT_LOOKUP)/v3/ct@$(CT_VERSION))

KIND         := $(LOCALBIN)/kind
KIND_VERSION := v0.27.0
KIND_LOOKUP  := kubernetes-sigs/kind
kind:
	@test -s $(KIND) && $(KIND) --version | grep -q $(KIND_VERSION) || \
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind/cmd/kind@$(KIND_VERSION))

KO           := $(LOCALBIN)/ko
KO_VERSION   := v0.17.1
KO_LOOKUP    := google/ko
ko:
	@test -s $(KO) && $(KO) -h | grep -q $(KO_VERSION) || \
	$(call go-install-tool,$(KO),github.com/$(KO_LOOKUP)@$(KO_VERSION))

GOLANGCI_LINT          := $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION  := v1.64.5
GOLANGCI_LINT_LOOKUP   := golangci/golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	@test -s $(GOLANGCI_LINT) && $(GOLANGCI_LINT) -h | grep -q $(GOLANGCI_LINT_VERSION) || \
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/$(GOLANGCI_LINT_LOOKUP)/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

APIDOCS_GEN         := $(LOCALBIN)/crdoc
APIDOCS_GEN_VERSION := v0.6.4
APIDOCS_GEN_LOOKUP  := fybrik/crdoc
apidocs-gen: ## Download crdoc locally if necessary.
	@test -s $(APIDOCS_GEN) && $(APIDOCS_GEN) --version | grep -q $(APIDOCS_GEN_VERSION) || \
	$(call go-install-tool,$(APIDOCS_GEN),fybrik.io/crdoc@$(APIDOCS_GEN_VERSION))

HARPOON         := $(LOCALBIN)/harpoon
HARPOON_VERSION := v0.9.6
HARPOON_LOOKUP  := alegrey91/harpoon
harpoon:
	@mkdir $(LOCALBIN)
	@curl -s https://raw.githubusercontent.com/alegrey91/harpoon/main/install | \
		sudo bash -s -- --install-version $(HARPOON_VERSION) --install-dir $(LOCALBIN)

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
[ -f $(1) ] || { \
    set -e ;\
    GOBIN=$(LOCALBIN) go install $(2) ;\
}
endef
