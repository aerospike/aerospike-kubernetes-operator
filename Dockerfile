# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.26@sha256:672c944cfd07599b5f1bb179869270cdc94c6254e41db4d431259693cb286b82 AS builder

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
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/
COPY errors/ errors/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GO111MODULE=on go build -a -o manager cmd/main.go

# Base image
FROM registry.access.redhat.com/ubi10/ubi-minimal:latest@sha256:bcecd3e74c9d03eb1a596c6c8f366775a289d422ac5925ec1539924d762ebf23

# Version of Operator (build arg)
ARG VERSION="4.6.0"

# User to run container as. Non-root, so the same image runs on OpenShift and
# other Kubernetes distributions. OpenShift replaces the UID with a random one
# in group 0, which is why /manager is group-owned by 0 below.
ARG USER="1001"

# Maintainer
LABEL maintainer="Aerospike <support@aerospike.com>"

# Labels
LABEL name="aerospike-kubernetes-operator" \
    vendor="Aerospike" \
    version="${VERSION}" \
    release="1" \
    summary="Aerospike Kubernetes Operator" \
    description="The Aerospike Kubernetes Operator automates the deployment and management of Aerospike enterprise clusters on Kubernetes" \
    io.k8s.display-name="Aerospike Kubernetes Operator v${VERSION}" \
    io.k8s.description="Aerospike Kubernetes Operator"

# Labels for RedHat Openshift platform
LABEL io.openshift.tags="database,nosql,aerospike" \
    io.openshift.non-scalable="false"

# License file
COPY LICENSE /licenses/

WORKDIR /

COPY --from=builder /workspace/manager .

RUN chgrp 0 /manager \
    && chmod g=u /manager

USER ${USER}

ENTRYPOINT ["/manager"]
