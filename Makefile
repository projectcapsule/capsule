.PHONY: k8s
k8s:
	operator-sdk generate k8s

.PHONY: crds
crds:
	operator-sdk generate crds

.PHONY: docker-image
docker-image:
	operator-sdk build quay.io/clastix/capsule:latest

.PHONY: goimports
goimports:
	goimports -w -l -local "github.com/clastix/capsule" .

.PHONY: golint
golint:
	golangci-lint run
