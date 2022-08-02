## DockerHub
### Build image

Make sure you update the release version of this image in the Dockerfile, and substitute the same in the following commands.

```shell
docker build --pull --no-cache -t aerospike/aerospike-kubernetes-init:<version>  .
```

### Push image
```shell
docker push aerospike/aerospike-kubernetes-init:<version>  
```

## RedHat OpenShift

### Build image

Make sure you update the release version of this image in the Dockerfile, and substitute the same in the following commands.

```shell
docker build --pull --no-cache --build-arg USER=1001 -t scan.connect.redhat.com/ospid-62b3f10d80219a05d423fadb/aerospile-kubernetes-init:<version> .
```

#### Login to redhat scan registry
Follow the login instructions on the RedHat `Aerospike Kubernetes Operator Init` certification project's `Set up Preflight` page.
Replace `podman` with docker in the instructions.

### Push image
```shell
docker push scan.connect.redhat.com/ospid-62b3f10d80219a05d423fadb/aerospile-kubernetes-init:<version>  
```

### Certify the image

Verify and submit the  image for RedHat OpenShift certification using the preflight tool.
Substitute the `pyxis-api-token` with the appropriate token obtained from RedHat portal.

```shell
preflight-linux-amd64 check container scan.connect.redhat.com/ospid-62b3f10d80219a05d423fadb/aerospile-kubernetes-init:<version>  --docker-config=$HOME/.docker/config.json --submit  --pyxis-api-token=<pyxis-api-token> --certification-project-id=ospid-62b3f10d80219a05d423fadb
```