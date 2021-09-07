#
# Aerospike Kubernetes Operator Init Container.
#

# Note: Don't change /workdir/bin path. This path is being referenced in operator codebase.

# Base image
FROM ubuntu:18.04

# Install dependencies and configmap exporter
RUN apt-get update \
    && apt-get install -y wget curl python3 \
    && mkdir -p /workdir/bin \
    && curl -L https://github.com/ashishshinde/kubernetes-configmap-exporter/releases/download/1.0.0/kubernetes-configmap-exporter -o /workdir/bin/kubernetes-configmap-exporter \
    && rm -f /var/cache/apt/archives/*.deb /var/cache/apt/archives/partial/*.deb /var/cache/apt/*.bin || true \
    && apt-get clean

# Add entrypoint script
ADD entrypoint.sh /workdir/bin/entrypoint.sh

# Add curl-amd64 binary
ADD https://github.com/moparisthebest/static-curl/releases/download/v7.77.0/curl-amd64 /workdir/bin/curl-amd64

# Update permissions
RUN chgrp -R 0 /workdir/ \
    && chmod -R gu=u+x /workdir/

# Add /workdir/bin to PATH
ENV PATH "/workdir/bin:$PATH"

# Entrypoint
ENTRYPOINT ["/workdir/bin/entrypoint.sh"]
