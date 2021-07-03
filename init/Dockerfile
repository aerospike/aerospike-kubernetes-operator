#
# Aerospike Kubernetes Operator Init Container.
#

FROM ubuntu:18.04

# Install dependencies.
RUN apt-get update && apt-get install -y wget curl python3

# Install configmap exporter
RUN curl -L https://github.com/ashishshinde/kubernetes-configmap-exporter/releases/download/1.0.0/kubernetes-configmap-exporter -o /usr/bin/kubernetes-configmap-exporter

ADD entrypoint.sh /usr/bin/entrypoint.sh
RUN chmod +x /usr/bin/entrypoint.sh

RUN rm -f /var/cache/apt/archives/*.deb /var/cache/apt/archives/partial/*.deb /var/cache/apt/*.bin || true
RUN apt-get clean

ENTRYPOINT ["/usr/bin/entrypoint.sh"]