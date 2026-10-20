#!/bin/bash
set -e

####################################
# Should be run from repository root
####################################

# Use the input operator image for testing if provided
BUNDLE_IMG=$1

# Optional docker config.json used to pull the bundle and operator images
# from a private registry (e.g. JFrog in CI)
PULL_CRED_PATH=${2:-}
PULL_SECRET_NAME="ako-image-pull"
OPERATOR_SA="aerospike-operator-controller-manager"

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Create storage classes.
case $(kubectl get nodes -o yaml) in
  *"cloud.google.com"*)
    echo "Installing ssd storage class for GKE."
    kubectl apply -f config/samples/storage/gke_ssd_storage_class.yaml
    ;;
  *"eks.amazonaws.com"*)
    echo "Installing ssd storage class for EKS."
    kubectl apply -f config/samples/storage/eks_ssd_storage_class.yaml
    ;;
  *)
    echo "Couldn't determine cloud provider from node list. Thus couldn't install 'ssd' storage class. Either install it manually or most likely ssd tests will fail."
    ;;
esac

IS_OPENSHIFT_CLUSTER=0
if kubectl get namespace | grep -o -a -m 1 -h openshift > /dev/null; then
  IS_OPENSHIFT_CLUSTER=1
fi

if [ $IS_OPENSHIFT_CLUSTER == 0 ]; then
  if ! operator-sdk olm status; then
    operator-sdk version
    operator-sdk olm install
  fi
fi

namespaces="test test1 test2 aerospike"
for namespace in $namespaces; do
  kubectl create namespace "$namespace" || true
  if [ $IS_OPENSHIFT_CLUSTER == 1 ]; then
    echo "Adding security constraints"
    oc adm policy add-scc-to-user anyuid system:serviceaccount:"$namespace":aerospike-operator-controller-manager
    # TODO: Find minimum privileges that should be granted
    oc adm policy add-scc-to-user privileged -z aerospike-operator-controller-manager -n $namespace
  fi
done

RUN_BUNDLE_ARGS=()
if [ -n "$PULL_CRED_PATH" ]; then
  echo "Configuring image pull secret $PULL_SECRET_NAME for the bundle and operator images"

  # The default service account appears shortly after its namespace.
  ATTEMPT=0
  until kubectl get serviceaccount default -n test > /dev/null 2>&1; do
    if [ $ATTEMPT -eq 30 ]; then
      echo "Service account default not found in namespace test"
      exit 1
    fi
    sleep 2
    ((ATTEMPT += 1))
  done

  "$DIR"/create_image_pull_secret.sh -n "$PULL_SECRET_NAME" -p "$PULL_CRED_PATH"

  # operator-sdk runs the bundle registry pod under the default service account.
  kubectl patch serviceaccount default -n test -p "{\"imagePullSecrets\":[{\"name\":\"$PULL_SECRET_NAME\"}]}"
  RUN_BUNDLE_ARGS+=(--pull-secret-name "$PULL_SECRET_NAME")

  # OLM creates the operator's service account during install, so it can't be
  # given the pull secret up front. Patch it as soon as it appears (and again if
  # OLM rewrites it), then restart operator pods created before the patch.
  (
    while true; do
      if secrets=$(kubectl get serviceaccount "$OPERATOR_SA" -n test -o jsonpath='{.imagePullSecrets[*].name}' 2>/dev/null) &&
        [[ " $secrets " != *" $PULL_SECRET_NAME "* ]]; then
        echo "Adding $PULL_SECRET_NAME to service account $OPERATOR_SA"
        kubectl patch serviceaccount "$OPERATOR_SA" -n test -p "{\"imagePullSecrets\":[{\"name\":\"$PULL_SECRET_NAME\"}]}" || true
        kubectl delete pod -n test -l control-plane=controller-manager --ignore-not-found || true
      fi
      sleep 5
    done
  ) &
  SA_WATCHER_PID=$!
  trap 'kill $SA_WATCHER_PID 2>/dev/null || true' EXIT
fi

operator-sdk run bundle "$BUNDLE_IMG"  --namespace=test --install-mode MultiNamespace=$(echo "$namespaces" | tr " " ",") --timeout=10m0s "${RUN_BUNDLE_ARGS[@]}"

echo "Enabling safe pod eviction in operator by patching the subscription"
kubectl patch subscription $(kubectl get subscriptions -n test -o jsonpath='{.items[?(@.metadata.name contains "aerospike")].metadata.name}') -n test --type='merge' -p '{"spec":{"config":{"env":[{"name":"ENABLE_SAFE_POD_EVICTION","value":"true"}]}}}'

for namespace in $namespaces; do
  ATTEMPT=0
  until [ $ATTEMPT -eq 15 ] || kubectl get csv -n "$namespace" | grep Succeeded; do
    sleep 2
    ((ATTEMPT += 1))
  done
done
sleep 10
