SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help
.NOTPARALLEL:

CLUSTER_NAME ?= capsule
PROXY_PORT ?= 9001
CONTEXT = kind-$(CLUSTER_NAME)
KUBECTL = kubectl --context $(CONTEXT)
TIMEOUT ?= 10m
ROOT_DIR := $(abspath $(CURDIR)/..)
CAPSULE_CHART ?= $(ROOT_DIR)/charts/capsule
DEV_IMAGE_REGISTRY ?= ko.local
DEV_IMAGE_REPOSITORY ?= capsule
DEV_IMAGE := $(DEV_IMAGE_REGISTRY)/$(DEV_IMAGE_REPOSITORY)
DEV_GIT_COMMIT := $(shell git -C '$(ROOT_DIR)' rev-parse --short HEAD)
ifeq ($(origin DEV_VERSION), undefined)
DEV_VERSION := dev-$(shell date -u +%Y%m%d%H%M%S)
endif
CAPSULE_POST_RENDERER := $(CURDIR)/scripts/capsule-post-renderer.sh
HELM_PLUGINS_DIR := $(CURDIR)/helm-plugins
GENERATED_DIR := $(CURDIR)/installation/.generated
DEX_CA_FILE := $(GENERATED_DIR)/ca.crt
AUTHENTICATION_CONFIG_FILE := $(GENERATED_DIR)/authentication-config.yaml
PLAYGROUND_CA_SHA256 = $(shell test -s '$(GENERATED_DIR)/ca.sha256' && sed -n '1p' '$(GENERATED_DIR)/ca.sha256')
PLAYGROUND_CA_BUNDLE = $(shell test -s '$(GENERATED_DIR)/ca.base64' && sed -n '1p' '$(GENERATED_DIR)/ca.base64')
CAPSULE_TLS_CERT_BUNDLE = $(shell test -s '$(GENERATED_DIR)/capsule.crt.base64' && sed -n '1p' '$(GENERATED_DIR)/capsule.crt.base64')
CAPSULE_TLS_KEY_BUNDLE = $(shell test -s '$(GENERATED_DIR)/capsule.key.base64' && sed -n '1p' '$(GENERATED_DIR)/capsule.key.base64')

-include .env

DEX_HOST ?= dex.capsule.local
HEADLAMP_HOST ?= headlamp.capsule.local
PROXY_HOST ?= proxy.capsule.local
GANGPLANK_HOST ?= gangplank.capsule.local
DEX_URL ?= https://$(DEX_HOST)
HEADLAMP_URL ?= https://$(HEADLAMP_HOST)
PROXY_URL ?= https://$(PROXY_HOST):$(PROXY_PORT)
GANGPLANK_URL ?= http://$(GANGPLANK_HOST)
OIDC_CLIENT_ID ?= kubernetes
OIDC_CLIENT_SECRET ?= kubernetes-secret
OIDC_USERNAME_CLAIM ?= name
OIDC_USERNAME_PREFIX ?=
OIDC_GROUPS_CLAIM ?= groups
OIDC_GROUPS_PREFIX ?= oidc:
LOCAL_HOSTS = $(DEX_HOST) $(HEADLAMP_HOST) $(PROXY_HOST) $(GANGPLANK_HOST)

export DEX_HOST HEADLAMP_HOST PROXY_HOST GANGPLANK_HOST
export DEX_URL HEADLAMP_URL PROXY_URL GANGPLANK_URL
export CLUSTER_NAME PROXY_PORT
export DEX_CA_FILE AUTHENTICATION_CONFIG_FILE
export PLAYGROUND_CA_SHA256 PLAYGROUND_CA_BUNDLE
export CAPSULE_TLS_CERT_BUNDLE CAPSULE_TLS_KEY_BUNDLE
export OIDC_CLIENT_ID OIDC_CLIENT_SECRET OIDC_USERNAME_CLAIM OIDC_USERNAME_PREFIX
export OIDC_GROUPS_CLAIM OIDC_GROUPS_PREFIX

SUBSTITUTION_VARIABLES := '$${CLUSTER_NAME} $${PROXY_PORT} $${DEX_HOST} $${HEADLAMP_HOST} $${PROXY_HOST} $${GANGPLANK_HOST} $${DEX_URL} $${HEADLAMP_URL} $${PROXY_URL} $${GANGPLANK_URL} $${DEX_CA_FILE} $${AUTHENTICATION_CONFIG_FILE} $${PLAYGROUND_CA_SHA256} $${PLAYGROUND_CA_BUNDLE} $${CAPSULE_TLS_CERT_BUNDLE} $${CAPSULE_TLS_KEY_BUNDLE} $${OIDC_CLIENT_ID} $${OIDC_CLIENT_SECRET} $${OIDC_USERNAME_CLAIM} $${OIDC_USERNAME_PREFIX} $${OIDC_GROUPS_CLAIM} $${OIDC_GROUPS_PREFIX}'

.PHONY: help check check-dev hosts certificates render-kind render-authentication cluster node-hosts flux apply render apply-platform apply-user wait-oidc configure-oidc wait status dev-image dev-capsule dev capsule-stable up down

help: ## Show the available playground targets.
	@awk 'BEGIN {FS = ":.*## "; print "Capsule local playground\n"} /^[a-zA-Z_-]+:.*## / {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: ## Check required local tools and the Docker daemon.
	@for tool in curl docker envsubst flux jq kind kubectl openssl; do \
		command -v "$$tool" >/dev/null || { echo "Missing required tool: $$tool" >&2; exit 1; }; \
	done
	@[[ '$(DEX_HOST)' =~ ^[a-zA-Z0-9.-]+$$ ]] || { echo "DEX_HOST is not a valid DNS hostname" >&2; exit 1; }
	@[[ '$(DEX_URL)' == 'https://$(DEX_HOST)' ]] || { echo "DEX_URL must be https://$(DEX_HOST) for local API-server OIDC" >&2; exit 1; }
	@[[ '$(HEADLAMP_URL)' == 'https://$(HEADLAMP_HOST)' ]] || { echo "HEADLAMP_URL must be https://$(HEADLAMP_HOST) for local ingress TLS" >&2; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "Docker is not running" >&2; exit 1; }

check-dev: check ## Check the additional tools and files needed for a local Capsule build.
	@for tool in go helm; do \
		command -v "$$tool" >/dev/null || { echo "Missing required development tool: $$tool" >&2; exit 1; }; \
	done
	@test -d '$(CAPSULE_CHART)' || { echo "Capsule chart not found: $(CAPSULE_CHART)" >&2; exit 1; }
	@test -x '$(CAPSULE_POST_RENDERER)' || { echo "Capsule post-renderer is not executable: $(CAPSULE_POST_RENDERER)" >&2; exit 1; }
	@test -f '$(HELM_PLUGINS_DIR)/capsule-playground/plugin.yaml' || { echo "Capsule Helm post-renderer plugin is missing" >&2; exit 1; }

hosts: ## Register the playground hostnames in the local /etc/hosts file.
	@for local_host in $(LOCAL_HOSTS); do \
		[[ "$$local_host" =~ ^[a-zA-Z0-9.-]+$$ ]] || { echo "Invalid local hostname: $$local_host" >&2; exit 1; }; \
		if awk -v host="$$local_host" '!/^[[:space:]]*#/ { for (field = 2; field <= NF; field++) if ($$field == host) found = 1 } END { exit !found }' /etc/hosts; then \
			echo "Already registered: $$local_host"; \
		else \
			printf '127.0.0.1\t%s\n' "$$local_host" | sudo tee -a /etc/hosts >/dev/null; \
			echo "Registered: $$local_host"; \
		fi; \
	done

certificates: ## Generate one persistent local CA and its service certificates.
	@set -eu; umask 077; mkdir -p '$(GENERATED_DIR)'; ca_changed=false; \
	if [[ ! -s '$(GENERATED_DIR)/ca.crt' || ! -s '$(GENERATED_DIR)/ca.key' ]]; then \
		printf '%s\n' '[req]' 'distinguished_name=dn' 'prompt=no' 'x509_extensions=v3_ca' '[dn]' 'CN=Capsule Playground CA' '[v3_ca]' 'basicConstraints=critical,CA:TRUE' 'keyUsage=critical,keyCertSign,cRLSign' 'subjectKeyIdentifier=hash' > '$(GENERATED_DIR)/ca.cnf'; \
		openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 3650 -config '$(GENERATED_DIR)/ca.cnf' -keyout '$(GENERATED_DIR)/ca.key' -out '$(GENERATED_DIR)/ca.crt' >/dev/null 2>&1; \
		ca_changed=true; \
		echo "Generated the persistent Capsule Playground CA" >&2; \
	fi; \
	openssl dgst -sha256 -r '$(GENERATED_DIR)/ca.crt' | awk '{print $$1}' > '$(GENERATED_DIR)/ca.sha256'; \
	openssl base64 -A -in '$(GENERATED_DIR)/ca.crt' > '$(GENERATED_DIR)/ca.base64'; \
	if $$ca_changed || [[ ! -s '$(GENERATED_DIR)/dex.crt' || ! -s '$(GENERATED_DIR)/dex.key' || ! -f '$(GENERATED_DIR)/hostname' || "$$(< '$(GENERATED_DIR)/hostname')" != '$(DEX_HOST) $(HEADLAMP_HOST)' ]] || ! openssl verify -CAfile '$(GENERATED_DIR)/ca.crt' '$(GENERATED_DIR)/dex.crt' >/dev/null 2>&1; then \
		printf '%s\n' '[req]' 'distinguished_name=dn' 'prompt=no' 'req_extensions=server' '[dn]' 'CN=$(DEX_HOST)' '[server]' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature,keyEncipherment' 'extendedKeyUsage=serverAuth' 'subjectAltName=DNS:$(DEX_HOST),DNS:$(HEADLAMP_HOST)' > '$(GENERATED_DIR)/dex.cnf'; \
		openssl req -new -newkey rsa:2048 -nodes -sha256 -config '$(GENERATED_DIR)/dex.cnf' -keyout '$(GENERATED_DIR)/dex.key' -out '$(GENERATED_DIR)/dex.csr' >/dev/null 2>&1; \
		openssl x509 -req -sha256 -days 825 -in '$(GENERATED_DIR)/dex.csr' -CA '$(GENERATED_DIR)/ca.crt' -CAkey '$(GENERATED_DIR)/ca.key' -set_serial 1 -extfile '$(GENERATED_DIR)/dex.cnf' -extensions server -out '$(GENERATED_DIR)/dex.crt' >/dev/null 2>&1; \
		printf '%s\n' '$(DEX_HOST) $(HEADLAMP_HOST)' > '$(GENERATED_DIR)/hostname'; \
		echo "Generated a local TLS certificate for $(DEX_HOST) and $(HEADLAMP_HOST)" >&2; \
	fi; \
	if $$ca_changed || [[ ! -s '$(GENERATED_DIR)/proxy.crt' || ! -s '$(GENERATED_DIR)/proxy.key' || ! -f '$(GENERATED_DIR)/proxy-hostname' || "$$(< '$(GENERATED_DIR)/proxy-hostname')" != '$(PROXY_HOST)' ]] || ! openssl verify -CAfile '$(GENERATED_DIR)/ca.crt' '$(GENERATED_DIR)/proxy.crt' >/dev/null 2>&1; then \
		printf '%s\n' '[req]' 'distinguished_name=dn' 'prompt=no' 'req_extensions=server' '[dn]' 'CN=capsule-proxy.capsule-system.svc' '[server]' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature,keyEncipherment' 'extendedKeyUsage=serverAuth' 'subjectAltName=DNS:localhost,DNS:$(PROXY_HOST),DNS:capsule-proxy,DNS:capsule-proxy.capsule-system.svc,IP:127.0.0.1' > '$(GENERATED_DIR)/proxy.cnf'; \
		openssl req -new -newkey rsa:2048 -nodes -sha256 -config '$(GENERATED_DIR)/proxy.cnf' -keyout '$(GENERATED_DIR)/proxy.key' -out '$(GENERATED_DIR)/proxy.csr' >/dev/null 2>&1; \
		openssl x509 -req -sha256 -days 825 -in '$(GENERATED_DIR)/proxy.csr' -CA '$(GENERATED_DIR)/ca.crt' -CAkey '$(GENERATED_DIR)/ca.key' -set_serial 2 -extfile '$(GENERATED_DIR)/proxy.cnf' -extensions server -out '$(GENERATED_DIR)/proxy.crt' >/dev/null 2>&1; \
		printf '%s\n' '$(PROXY_HOST)' > '$(GENERATED_DIR)/proxy-hostname'; \
		echo "Generated a local TLS certificate for Capsule Proxy" >&2; \
	fi; \
	if $$ca_changed || [[ ! -s '$(GENERATED_DIR)/capsule.crt' || ! -s '$(GENERATED_DIR)/capsule.key' ]] || ! openssl verify -CAfile '$(GENERATED_DIR)/ca.crt' '$(GENERATED_DIR)/capsule.crt' >/dev/null 2>&1; then \
		printf '%s\n' '[req]' 'distinguished_name=dn' 'prompt=no' 'req_extensions=server' '[dn]' 'CN=capsule-webhook-service.capsule-system.svc' '[server]' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature,keyEncipherment' 'extendedKeyUsage=serverAuth' 'subjectAltName=DNS:capsule-webhook-service.capsule-system.svc,DNS:capsule-webhook-service.capsule-system.svc.cluster.local' > '$(GENERATED_DIR)/capsule.cnf'; \
		openssl req -new -newkey rsa:2048 -nodes -sha256 -config '$(GENERATED_DIR)/capsule.cnf' -keyout '$(GENERATED_DIR)/capsule.key' -out '$(GENERATED_DIR)/capsule.csr' >/dev/null 2>&1; \
		openssl x509 -req -sha256 -days 825 -in '$(GENERATED_DIR)/capsule.csr' -CA '$(GENERATED_DIR)/ca.crt' -CAkey '$(GENERATED_DIR)/ca.key' -set_serial 3 -extfile '$(GENERATED_DIR)/capsule.cnf' -extensions server -out '$(GENERATED_DIR)/capsule.crt' >/dev/null 2>&1; \
		echo "Generated a local TLS certificate for the Capsule webhook" >&2; \
	fi; \
	openssl base64 -A -in '$(GENERATED_DIR)/capsule.crt' > '$(GENERATED_DIR)/capsule.crt.base64'; \
	openssl base64 -A -in '$(GENERATED_DIR)/capsule.key' > '$(GENERATED_DIR)/capsule.key.base64'; \
	if [[ ! -f '$(AUTHENTICATION_CONFIG_FILE)' ]]; then \
		printf '%s\n' 'apiVersion: apiserver.config.k8s.io/v1' 'kind: AuthenticationConfiguration' 'jwt: []' > '$(AUTHENTICATION_CONFIG_FILE)'; \
	fi

render-kind: certificates ## Render the kind configuration with OIDC settings.
	@envsubst $(SUBSTITUTION_VARIABLES) < kind.config.yaml

render-authentication: certificates ## Render the API-server OIDC authentication config.
	@printf '%s\n' 'apiVersion: apiserver.config.k8s.io/v1' 'kind: AuthenticationConfiguration' 'jwt:' '  - issuer:' '      url: "$(DEX_URL)"' '      audiences:' '        - "$(OIDC_CLIENT_ID)"' '      certificateAuthority: |'
	@sed 's/^/        /' '$(DEX_CA_FILE)'
	@printf '%s\n' '    claimMappings:' '      username:' '        claim: "$(OIDC_USERNAME_CLAIM)"' '        prefix: "$(OIDC_USERNAME_PREFIX)"' '      groups:' '        claim: "$(OIDC_GROUPS_CLAIM)"' '        prefix: "$(OIDC_GROUPS_PREFIX)"'

cluster: check certificates ## Create the kind cluster unless it already exists.
	@if kind get clusters 2>/dev/null | grep -qx '$(CLUSTER_NAME)'; then \
		docker exec '$(CLUSTER_NAME)-control-plane' grep -Fq -- '--authentication-config=/etc/kubernetes/pki/playground-authentication-config.yaml' /etc/kubernetes/manifests/kube-apiserver.yaml || { echo "Existing cluster does not use the playground authentication config; run make down, then make up" >&2; exit 1; }; \
		echo "kind cluster $(CLUSTER_NAME) already exists with reloadable authentication"; \
	else \
		printf '%s\n' 'apiVersion: apiserver.config.k8s.io/v1' 'kind: AuthenticationConfiguration' 'jwt: []' > '$(AUTHENTICATION_CONFIG_FILE)'; \
		kind_config="$$(mktemp)"; trap 'rm -f "$$kind_config"' EXIT; \
		envsubst $(SUBSTITUTION_VARIABLES) < kind.config.yaml > "$$kind_config"; \
		kind create cluster --config "$$kind_config"; \
	fi
	@$(MAKE) node-hosts

node-hosts: ## Make the Dex ingress host reachable from the API-server static Pod.
	@worker_ip="$$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' '$(CLUSTER_NAME)-worker')"; \
	test -n "$$worker_ip" || { echo "Could not determine the kind worker IP" >&2; exit 1; }; \
	for node in $$(kind get nodes --name '$(CLUSTER_NAME)'); do \
		docker exec "$$node" grep -Fq '$(DEX_HOST)' /etc/hosts || docker exec "$$node" sh -c 'printf "%s\n" "$$1" >> /etc/hosts' sh "$$worker_ip $(DEX_HOST)"; \
	done; \
	manifest=/etc/kubernetes/manifests/kube-apiserver.yaml; \
	if ! docker exec '$(CLUSTER_NAME)-control-plane' grep -Fq '    - "$(DEX_HOST)"' "$$manifest"; then \
		if docker exec '$(CLUSTER_NAME)-control-plane' grep -q '^  hostAliases:' "$$manifest"; then \
			docker exec '$(CLUSTER_NAME)-control-plane' sed -i "/^  hostAliases:$$/a\\  - ip: \"$$worker_ip\"\\n    hostnames:\\n    - \"$(DEX_HOST)\"" "$$manifest"; \
		else \
			docker exec '$(CLUSTER_NAME)-control-plane' sed -i "/^  hostNetwork: true$$/i\\  hostAliases:\\n  - ip: \"$$worker_ip\"\\n    hostnames:\\n    - \"$(DEX_HOST)\"" "$$manifest"; \
		fi; \
	fi; \
	for attempt in $$(seq 1 60); do \
		$(KUBECTL) get --raw /readyz >/dev/null 2>&1 && exit 0; \
		sleep 1; \
	done; \
	echo "API server did not become ready after configuring the Dex host alias" >&2; \
	exit 1

flux: ## Install the Flux controllers in the local cluster.
	@flux install --context $(CONTEXT)

render: certificates ## Render the installation manifests with environment substitution.
	@kubectl kustomize installation | envsubst $(SUBSTITUTION_VARIABLES)

apply: certificates ## Apply the rendered installation to the local cluster.
	@kubectl kustomize installation | envsubst $(SUBSTITUTION_VARIABLES) | $(KUBECTL) apply -f -

apply-headlamp:  ## Apply the rendered installation to the local cluster.
	@kubectl kustomize installation | envsubst $(SUBSTITUTION_VARIABLES) | $(KUBECTL) apply -f -


apply-platform:
	@kubectl kustomize platform | envsubst $(SUBSTITUTION_VARIABLES) | $(KUBECTL) apply -f -

apply-user:
	@$(KUBECTL) get ns wind-uat >/dev/null 2>&1 || $(KUBECTL) create ns wind-uat --as gatsby --as-group projectcapsule.dev
	@$(KUBECTL) label ns wind-uat env=test --overwrite
	@$(KUBECTL) get ns wind-test >/dev/null 2>&1 || $(KUBECTL) create ns wind-test --as gatsby --as-group projectcapsule.dev
	@$(KUBECTL) label ns wind-test env=test --overwrite
	@$(KUBECTL) get ns wind-prod >/dev/null 2>&1 || $(KUBECTL) create ns wind-prod --as gatsby --as-group projectcapsule.dev
	@$(KUBECTL) label ns wind-prod env=prod --overwrite
	@$(KUBECTL) get ns green-uat >/dev/null 2>&1 || $(KUBECTL) create ns green-uat --as bob --as-group projectcapsule.dev
	@$(KUBECTL) label ns green-uat env=test --overwrite
	@$(KUBECTL) get ns green-test >/dev/null 2>&1 || $(KUBECTL) create ns green-test --as bob --as-group projectcapsule.dev
	@$(KUBECTL) label ns green-test env=test --overwrite
	@$(KUBECTL) get ns green-prod >/dev/null 2>&1 || $(KUBECTL) create ns green-prod --as bob --as-group projectcapsule.dev
	@$(KUBECTL) label ns green-prod env=prod --overwrite
	@$(KUBECTL) get ns solar-uat >/dev/null 2>&1 || $(KUBECTL) create ns solar-uat --as alice --as-group projectcapsule.dev
	@$(KUBECTL) label ns solar-uat env=test --overwrite
	@$(KUBECTL) get ns solar-test >/dev/null 2>&1 || $(KUBECTL) create ns solar-test --as alice --as-group projectcapsule.dev
	@$(KUBECTL) label ns solar-test env=test --overwrite
	@$(KUBECTL) get ns solar-prod >/dev/null 2>&1 || $(KUBECTL) create ns solar-prod --as alice --as-group projectcapsule.dev
	@$(KUBECTL) label ns solar-prod env=prod --overwrite
	@kubectl kustomize user | envsubst $(SUBSTITUTION_VARIABLES) | $(KUBECTL) apply -f -


wait-oidc: certificates ## Wait for ingress and Dex before enabling API-server OIDC.
	@$(KUBECTL) wait --namespace flux-system --for=condition=ready helmrelease/ingress-nginx helmrelease/dex --timeout=$(TIMEOUT)
	@for attempt in $$(seq 1 60); do \
		issuer="$$(curl --fail --silent --show-error --cacert '$(DEX_CA_FILE)' --resolve '$(DEX_HOST):443:127.0.0.1' '$(DEX_URL)/.well-known/openid-configuration' 2>/dev/null | jq --raw-output '.issuer // empty' 2>/dev/null)" || true; \
		if [[ "$$issuer" == '$(DEX_URL)' ]]; then \
			echo "Dex discovery endpoint is ready"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "Dex discovery endpoint did not advertise $(DEX_URL) within 60 seconds" >&2; \
	exit 1

configure-oidc: certificates ## Enable the API-server JWT authenticator after Dex is reachable.
	@issuer="$$(curl --fail --silent --show-error --cacert '$(DEX_CA_FILE)' --resolve '$(DEX_HOST):443:127.0.0.1' '$(DEX_URL)/.well-known/openid-configuration' | jq --raw-output '.issuer')"; \
	[[ "$$issuer" == '$(DEX_URL)' ]] || { echo "Dex advertised unexpected issuer: $$issuer" >&2; exit 1; }
	@$(MAKE) --no-print-directory render-authentication > '$(AUTHENTICATION_CONFIG_FILE)'
	@docker exec '$(CLUSTER_NAME)-control-plane' grep -Fq 'url: "$(DEX_URL)"' /etc/kubernetes/pki/playground-authentication-config.yaml
	@expected_hash="$$(openssl dgst -sha256 -r '$(AUTHENTICATION_CONFIG_FILE)' | awk '{print $$1}')"; \
	for attempt in $$(seq 1 120); do \
		metrics="$$( $(KUBECTL) get --raw /metrics )"; \
		if grep -Eq "^apiserver_authentication_config_controller_last_config_info\\{[^}]*hash=\\\"sha256:$$expected_hash\\\"[^}]*\\} 1$$" <<< "$$metrics"; then \
			echo "Enabled API-server OIDC authentication for $(DEX_URL)"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "API server did not activate authentication config sha256:$$expected_hash within 120 seconds" >&2; \
	exit 1

wait: ## Wait until every playground Helm release is ready.
	@$(KUBECTL) wait --namespace flux-system --for=condition=ready helmrelease --all --timeout=$(TIMEOUT)

status: ## Show nodes, Helm releases, ingresses, and pods.
	@$(KUBECTL) get nodes
	@$(KUBECTL) get helmreleases --namespace flux-system
	@$(KUBECTL) get ingress --all-namespaces
	@$(KUBECTL) get pods --all-namespaces

dev-image: check-dev ## Build Capsule from the current checkout and load it into kind.
	@$(MAKE) --no-print-directory -C '$(ROOT_DIR)' ko-build-capsule CAPSULE_IMG='$(DEV_IMAGE)' VERSION='$(DEV_VERSION)' GIT_TAG_COMMIT='$(DEV_GIT_COMMIT)'
	@kind load docker-image '$(DEV_IMAGE):$(DEV_VERSION)' --name '$(CLUSTER_NAME)'

dev-capsule: check-dev certificates ## Deploy the current Capsule source and chart into a running playground.
	@$(KUBECTL) get helmrelease capsule --namespace flux-system >/dev/null 2>&1 || { echo "Capsule is not installed; run make up first" >&2; exit 1; }
	@helm status capsule --kube-context '$(CONTEXT)' --namespace capsule-system >/dev/null 2>&1 || { echo "The Capsule Helm release is not ready; run make up first" >&2; exit 1; }
	@$(MAKE) --no-print-directory dev-image
	@flux suspend helmrelease capsule --context '$(CONTEXT)' --namespace flux-system
	@set -e; values_file="$$(mktemp)"; \
		cleanup() { result=$$?; rm -f "$$values_file"; if [[ $$result -ne 0 ]]; then echo "Local Helm upgrade failed; resuming the Flux-managed Capsule release" >&2; flux resume helmrelease capsule --context '$(CONTEXT)' --namespace flux-system --wait --timeout '$(TIMEOUT)' || true; fi; exit $$result; }; \
		trap cleanup EXIT; \
		helm get values capsule --kube-context '$(CONTEXT)' --namespace capsule-system --output yaml > "$$values_file"; \
		post_renderer='$(CAPSULE_POST_RENDERER)'; \
		if [[ "$$(helm version --template '{{.Version}}')" == v4.* ]]; then export HELM_PLUGINS='$(HELM_PLUGINS_DIR)'; post_renderer='capsule-playground'; fi; \
		helm upgrade capsule '$(CAPSULE_CHART)' \
			--kube-context '$(CONTEXT)' \
			--namespace capsule-system \
			--dependency-update \
			--values "$$values_file" \
			--set-string 'manager.image.registry=$(DEV_IMAGE_REGISTRY)' \
			--set-string 'manager.image.repository=$(DEV_IMAGE_REPOSITORY)' \
			--set-string 'manager.image.tag=$(DEV_VERSION)' \
			--set 'manager.image.pullPolicy=Never' \
			--post-renderer "$$post_renderer" \
			--wait \
			--wait-for-jobs \
			--timeout '$(TIMEOUT)'
	@$(KUBECTL) rollout restart deployment/capsule-controller-manager --namespace capsule-system
	@$(KUBECTL) rollout status deployment/capsule-controller-manager --namespace capsule-system --timeout='$(TIMEOUT)'
	@echo "Deployed local Capsule image $(DEV_IMAGE):$(DEV_VERSION)"

dev: up ## Bring up the playground and deploy Capsule from the current checkout.
	@$(MAKE) --no-print-directory dev-capsule

capsule-stable: check ## Restore the Flux-managed Capsule release.
	@if ! $(KUBECTL) get helmrelease capsule --namespace flux-system >/dev/null 2>&1; then \
		echo "Recreating the Flux-managed Capsule HelmRelease..."; \
		$(MAKE) --no-print-directory apply; \
	fi
	@flux resume helmrelease capsule --context '$(CONTEXT)' --namespace flux-system --wait --timeout '$(TIMEOUT)'
	@flux reconcile helmrelease capsule --context '$(CONTEXT)' --namespace flux-system --with-source --timeout '$(TIMEOUT)'
	@$(KUBECTL) rollout status deployment/capsule-controller-manager --namespace capsule-system --timeout='$(TIMEOUT)'
	@echo "Restored the Flux-managed Capsule release"

up: cluster ## Install Flux and bring up the local playground.
	@$(MAKE) flux
	@$(MAKE) apply
	@$(MAKE) wait-oidc
	@$(MAKE) configure-oidc
	@$(MAKE) wait
	@$(MAKE) apply-platform
	@$(MAKE) apply-user
	@echo
	@echo "Headlamp: $(HEADLAMP_URL)"
	@echo "Dex:      $(DEX_URL)"
	@echo "Proxy:    $(PROXY_URL)"

down: ## Delete the local kind cluster.
	@kind delete cluster --name $(CLUSTER_NAME)
