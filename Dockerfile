# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.19 as builder

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
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY errors/ errors/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GO111MODULE=on go build -a -o manager main.go

# Base image
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Version of Operator (build arg)
ARG VERSION="3.0.0"

# User to run container as
ARG USER="root"

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
