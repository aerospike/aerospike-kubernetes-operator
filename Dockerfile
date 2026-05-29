# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.25@sha256:cd05a378aaf011e8056745363e5c40f4f2bef0fa4d9bf19b9c38316079c332ff AS builder

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
FROM registry.access.redhat.com/ubi10/ubi-minimal:latest@sha256:6edd84087c7c05b9464de987d9a9bc210be1ec9c426aa6c5aaff39aa32f0bbc3

# Version of Operator (build arg)
ARG VERSION="4.4.1"

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
