
#!/usr/bin/env bash

# Setup operator
kubectl create namespace aerospike
sleep 2

kubectl apply -f deploy/storage_class.yaml
sleep 2

kubectl apply -f deploy/crds/aerospike.com_aerospikeclusters_crd.yaml
sleep 2

kubectl apply -f deploy/rbac.yaml
sleep 2

kubectl apply -f deploy/operator.yaml
sleep 10

# prereq for cluster

# kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike
# sleep 2

# kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike
# sleep 2

#### DataInMemory without persistent Cluster
# kubectl apply -f deploy/samples/dim_nostorage_cluster_cr.yaml

# #### HDD and DataInMemory storage Cluster
# kubectl apply -f deploy/samples/hdd_dim_storage_cluster_cr.yaml

# #### HDD and DataInIndex storage Cluster
# kubectl apply -f deploy/samples/hdd_dii_storage_cluster_cr.yaml

# #### SSD storage Cluster
# kubectl apply -f deploy/samples/ssd_storage_cluster_cr.yaml

# #### TLS enabled cluster (Client-Server, Heartbeat, Fabric) Cluster
# kubectl apply -f deploy/samples/tls_cluster_cr.yaml
