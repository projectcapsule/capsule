# Build the manager binary
FROM golang:1.13 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY version.go version.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY .git .git

# Build
RUN git config --get remote.origin.url > /tmp/GIT_REPO && \
    git rev-parse --short HEAD > /tmp/GIT_HEAD_COMMIT && \
    git describe --abbrev=0 --tags > /tmp/GIT_LAST_TAG && \
    git rev-parse --short $(cat /tmp/GIT_LAST_TAG) > /tmp/GIT_TAG_COMMIT && \
    git diff $(cat /tmp/GIT_HEAD_COMMIT) $(cat /tmp/GIT_TAG_COMMIT) --quiet > /tmp/GIT_MODIFIED1 || echo '.dev' > /tmp/GIT_MODIFIED1 && \
    git diff --quiet > /tmp/GIT_MODIFIED2 || echo '.dirty' > /tmp/GIT_MODIFIED2 && \
    cat /tmp/GIT_MODIFIED1 /tmp/GIT_MODIFIED2 | tr -d '\n' > /tmp/GIT_MODIFIED && \
    date '+%Y-%m-%dT%H:%M:%S' > /tmp/BUILD_DATE &&\
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build \
        -gcflags "-N -l" \
        -ldflags "-X main.GitRepo=$(cat /tmp/GIT_REPO) -X main.GitTag=$(cat /tmp/GIT_LAST_TAG) -X main.GitCommit=$(cat /tmp/GIT_HEAD_COMMIT) -X main.GitDirty=$(cat /tmp/GIT_MODIFIED) -X main.BuildTime=$(cat /tmp/BUILD_DATE)" \
        -o manager

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER nonroot:nonroot

ENTRYPOINT ["/manager"]
