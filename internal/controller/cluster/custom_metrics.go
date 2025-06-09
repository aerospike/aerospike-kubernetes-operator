package cluster

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

const (
	clusterLabelKey   = "cluster_name"
	namespaceLabelKey = "cluster_namespace"
	phaseLabelKey     = "phase"
)

// aerospikeClusterPhase is a custom metric that tracks the phase of AerospikeCluster CRs.
var aerospikeClusterPhase = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "aerospike_ako_aerospikecluster_phase",
		Help: "Phase of AerospikeCluster CRs",
	},
	[]string{clusterLabelKey, namespaceLabelKey, phaseLabelKey},
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(aerospikeClusterPhase)
}

var phases = []asdbv1.AerospikeClusterPhase{
	asdbv1.AerospikeClusterInProgress,
	asdbv1.AerospikeClusterError,
	asdbv1.AerospikeClusterCompleted,
}

// It sets the metric value to 1.0 for the current phase of the AerospikeCluster
// and 0.0 for all other phases.
func (r *SingleClusterReconciler) addClusterPhaseMetric() {
	for _, phase := range phases {
		value := 0.0

		if r.aeroCluster.Status.Phase == phase {
			value = 1.0
		}

		aerospikeClusterPhase.WithLabelValues(
			r.aeroCluster.Name,
			r.aeroCluster.Namespace,
			string(phase)).Set(value)
	}
}

// It removes the metric for the AerospikeCluster CR with the specified name and namespace.
func (r *SingleClusterReconciler) removeClusterPhaseMetric() {
	aerospikeClusterPhase.DeletePartialMatch(
		prometheus.Labels{
			clusterLabelKey:   r.aeroCluster.Name,
			namespaceLabelKey: r.aeroCluster.Namespace,
		},
	)
}
