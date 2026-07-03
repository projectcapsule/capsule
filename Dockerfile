# Build the manager binary
FROM golang:1.26.4-bookworm as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

ARG TARGETARCH
ARG GIT_HEAD_COMMIT
ARG GIT_TAG_COMMIT
ARG GIT_LAST_TAG
ARG GIT_MODIFIED
ARG GIT_REPO
ARG BUILD_DATE

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH GO111MODULE=on go build \
        -gcflags "-N -l" \
        -ldflags "-X github.com/projectcapsule/capsule/internal/version.GitRepo=$GIT_REPO \
                  -X github.com/projectcapsule/capsule/internal/version.GitTag=$GIT_LAST_TAG \
                  -X github.com/projectcapsule/capsule/internal/version.GitCommit=$GIT_HEAD_COMMIT \
                  -X github.com/projectcapsule/capsule/internal/version.GitDirty=$GIT_MODIFIED \
                  -X github.com/projectcapsule/capsule/internal/version.BuildTime=$BUILD_DATE" \
        -o manager ./cmd/controller/

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:3.22
# Upgrade packages for vulnerabilities
RUN apk update && apk upgrade

WORKDIR /
COPY --from=builder /workspace/manager .

# add new user
ARG USER=nonroot
ENV HOME /home/$USER
RUN adduser -D $USER \
        && mkdir -p /etc/sudoers.d \
        && echo "$USER ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/$USER \
        && chmod 0440 /etc/sudoers.d/$USER
USER $USER:$USER

ENTRYPOINT ["/manager"]
