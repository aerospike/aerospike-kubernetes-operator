package cluster

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

const (
	clusterLabelKey     = "cluster_name"
	namespaceLabelKey   = "cluster_namespace"
	phaseLabelKey       = "phase"
	restartTypeLabelKey = "type"
	triggerLabelKey     = "trigger"
	resultLabelKey      = "result"
)

// Label values of aerospike_ako_pod_restarts_total.
const (
	restartTypeWarm        = "warm"
	restartTypeCold        = "cold"
	triggerPlanned         = "planned"
	triggerUpgrade         = "upgrade"
	triggerFailureRecovery = "failure_recovery"
)

// Label values of aerospike_ako_dynamic_config_updates_total.
const (
	dynamicConfigResultSuccess         = "success"
	dynamicConfigResultFailed          = "failed"
	dynamicConfigResultPartiallyFailed = "partially_failed"
)

// aerospikeClusterPhase is a custom metric that tracks the phase of AerospikeCluster CRs.
var aerospikeClusterPhase = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "aerospike_ako_aerospikecluster_phase",
		Help: "Phase of AerospikeCluster CRs",
	},
	[]string{clusterLabelKey, namespaceLabelKey, phaseLabelKey},
)

// podRestartsTotal counts the Aerospike pods restarted by the operator.
var podRestartsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "aerospike_ako_pod_restarts_total",
		Help: "Number of Aerospike pod restarts performed by the operator, " +
			"by restart type (warm, cold) and trigger (planned, upgrade, failure_recovery)",
	},
	[]string{clusterLabelKey, namespaceLabelKey, restartTypeLabelKey, triggerLabelKey},
)

// dynamicConfigUpdatesTotal counts the dynamic config updates the operator applied to Aerospike pods.
var dynamicConfigUpdatesTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "aerospike_ako_dynamic_config_updates_total",
		Help: "Number of per-pod dynamic config updates sent to the Aerospike server by the operator, " +
			"by result (success, failed, partially_failed)",
	},
	[]string{clusterLabelKey, namespaceLabelKey, resultLabelKey},
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(aerospikeClusterPhase, podRestartsTotal, dynamicConfigUpdatesTotal)
}

var phases = []asdbv1.AerospikeClusterPhase{
	asdbv1.AerospikeClusterInProgress,
	asdbv1.AerospikeClusterError,
	asdbv1.AerospikeClusterCompleted,
}

// podRestartLabelValues lists the type and trigger pairs the operator can count. An upgrade always
// deletes the pod, so there is no warm upgrade.
var podRestartLabelValues = [][2]string{
	{restartTypeWarm, triggerPlanned},
	{restartTypeWarm, triggerFailureRecovery},
	{restartTypeCold, triggerPlanned},
	{restartTypeCold, triggerUpgrade},
	{restartTypeCold, triggerFailureRecovery},
}

var dynamicConfigResults = []string{
	dynamicConfigResultSuccess,
	dynamicConfigResultFailed,
	dynamicConfigResultPartiallyFailed,
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

	r.initClusterCounterMetrics()
}

// initClusterCounterMetrics creates the counter series at 0 so that increase() sees their first
// increment. Add(0) leaves an existing series unchanged.
func (r *SingleClusterReconciler) initClusterCounterMetrics() {
	for _, labels := range podRestartLabelValues {
		podRestartsTotal.WithLabelValues(r.aeroCluster.Name, r.aeroCluster.Namespace, labels[0], labels[1]).Add(0)
	}

	for _, result := range dynamicConfigResults {
		dynamicConfigUpdatesTotal.WithLabelValues(r.aeroCluster.Name, r.aeroCluster.Namespace, result).Add(0)
	}
}

// podRestartTrigger returns the trigger label value of a pod restart. A failed pod is always
// failure_recovery, also when it is replaced as part of an image upgrade.
func podRestartTrigger(isFailureRecovery, isUpgrade bool) string {
	switch {
	case isFailureRecovery:
		return triggerFailureRecovery
	case isUpgrade:
		return triggerUpgrade
	default:
		return triggerPlanned
	}
}

// incPodRestartMetric counts one pod restart. Restart types that do not restart the pod
// (noRestart, noRestartUpdateConf) are not counted.
func (r *SingleClusterReconciler) incPodRestartMetric(restartType RestartType, trigger string) {
	var restartTypeValue string

	switch restartType {
	case quickRestart:
		restartTypeValue = restartTypeWarm
	case podRestart:
		restartTypeValue = restartTypeCold
	case noRestart, noRestartUpdateConf:
		return
	}

	podRestartsTotal.WithLabelValues(
		r.aeroCluster.Name,
		r.aeroCluster.Namespace,
		restartTypeValue,
		trigger).Inc()
}

// incDynamicConfigUpdateMetric counts one dynamic config update on a pod with the given result.
func (r *SingleClusterReconciler) incDynamicConfigUpdateMetric(result string) {
	dynamicConfigUpdatesTotal.WithLabelValues(
		r.aeroCluster.Name,
		r.aeroCluster.Namespace,
		result).Inc()
}

// dynamicConfigResultFromStatus maps a failed dynamicConfigUpdateStatus to its result label value.
func dynamicConfigResultFromStatus(status asdbv1.DynamicConfigUpdateStatus) string {
	if status == asdbv1.PartiallyFailed {
		return dynamicConfigResultPartiallyFailed
	}

	return dynamicConfigResultFailed
}

// removeClusterMetrics removes all custom metrics of the AerospikeCluster CR with the specified name and namespace.
func (r *SingleClusterReconciler) removeClusterMetrics() {
	clusterLabels := prometheus.Labels{
		clusterLabelKey:   r.aeroCluster.Name,
		namespaceLabelKey: r.aeroCluster.Namespace,
	}

	aerospikeClusterPhase.DeletePartialMatch(clusterLabels)
	podRestartsTotal.DeletePartialMatch(clusterLabels)
	dynamicConfigUpdatesTotal.DeletePartialMatch(clusterLabels)
}
