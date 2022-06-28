#
# Aerospike Kubernetes Operator Init Container.
#

# Note: Don't change /workdir/bin path. This path is being referenced in operator codebase.

# Base image
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

# Maintainer
LABEL maintainer="Aerospike, Inc. <developers@aerospike.com>"

ARG VERSION=0.0.16
ARG USER=root
ARG DESCRIPTION="Initializes Aerospike pods created but the Aerospike Kubernetes Operator. Initialization includes setting up devices."

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

# License file
COPY LICENSE /licenses/

# Install dependencies and configmap exporter
RUN microdnf update -y \
    && microdnf install wget python3 curl findutils util-linux procps -y \
    && mkdir -p /workdir/bin \
    && curl -L https://github.com/ashishshinde/kubernetes-configmap-exporter/releases/download/1.0.0/kubernetes-configmap-exporter -o /workdir/bin/kubernetes-configmap-exporter \
    # Update permissions
    && chgrp -R 0 /workdir \
    && chmod -R g=u+x /workdir \
    # Cleanup
    && microdnf clean all

# Add /workdir/bin to PATH
ENV PATH "/workdir/bin:$PATH"

# For RedHat Openshift, set this to non-root user ID 1001 using
# --build-arg USER=1001 as docker build argument.
USER $USER

# Entrypoint
ENTRYPOINT ["/workdir/bin/entrypoint.sh"]
