FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/aerospike-kubernetes-operator \
    USER_UID=1001 \
    USER_NAME=aerospike-kubernetes-operator

ARG OPERATOR_VERSION
RUN if [ -z "$OPERATOR_VERSION" ]; then echo "OPERATOR_VERSION NOT SET - ERROR"; exit 1; else : ; fi
ENV OPERATOR_VERSION=$OPERATOR_VERSION

# install operator binary
COPY build/_output/bin/aerospike-kubernetes-operator ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
