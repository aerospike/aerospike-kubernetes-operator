#
# Aerospike Kubernetes Operator Init Container.
#
# Build the akoinit binary
FROM --platform=$BUILDPLATFORM golang:1.18 as builder

# OS and Arch args
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GO111MODULE=on go build -a -o akoinit main.go
# Note: Don't change /workdir/bin path. This path is being referenced in operator codebase.

# Base image
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

# Maintainer
LABEL maintainer="Aerospike, Inc. <developers@aerospike.com>"

ARG VERSION=0.0.19
ARG USER=root
ARG DESCRIPTION="Initializes Aerospike pods created by the Aerospike Kubernetes Operator. Initialization includes setting up devices."

# Labels
LABEL name="aerospike-kubernetes-init" \
  vendor="Aerospike" \
  version=$VERSION \
  release="1" \
  summary="Aerospike Kubernetes Operator Init" \
  description=$DESCRIPTION \
  io.k8s.display-name="Aerospike Kubernetes Operator Init $VERSION" \
  io.openshift.tags="database,nosql,aerospike" \
  io.k8s.description=$DESCRIPTION \
  io.openshift.non-scalable="false"

# Add entrypoint script
ADD entrypoint.sh /workdir/bin/entrypoint.sh
COPY --from=builder /workspace/akoinit /workdir/bin/

# License file
COPY LICENSE /licenses/

# Install dependencies and configmap exporter
RUN microdnf update -y \
    && microdnf install findutils util-linux -y \
    && mkdir -p /workdir/bin \
    # Update permissions
    && chgrp -R 0 /workdir \
    && chmod -R g=u+x,o=o+x /workdir \
    # Cleanup
    && microdnf clean all

# Add /workdir/bin to PATH
ENV PATH "/workdir/bin:$PATH"

# For RedHat Openshift, set this to non-root user ID 1001 using
# --build-arg USER=1001 as docker build argument.
USER $USER

# Entrypoint
ENTRYPOINT ["/workdir/bin/entrypoint.sh"]
