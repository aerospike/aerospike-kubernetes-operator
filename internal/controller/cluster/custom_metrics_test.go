package cluster

import (
	"testing"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// newMetricsTestReconciler returns a reconciler for a cluster with the given name. Each test case
// sets its own cluster name so that its series in the package-global vecs are isolated.
func newMetricsTestReconciler(cluster string) *SingleClusterReconciler {
	ac := getMinimalCluster()
	ac.Name = cluster

	return &SingleClusterReconciler{aeroCluster: ac}
}

func TestPodRestartTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		want              string
		isFailureRecovery bool
		isUpgrade         bool
	}{
		{name: "planned restart", want: triggerPlanned},
		{name: "upgrade", isUpgrade: true, want: triggerUpgrade},
		{name: "failed pod restart", isFailureRecovery: true, want: triggerFailureRecovery},
		{
			name:              "failed pod replaced during upgrade stays failure recovery",
			isFailureRecovery: true,
			isUpgrade:         true,
			want:              triggerFailureRecovery,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := podRestartTrigger(tc.isFailureRecovery, tc.isUpgrade); got != tc.want {
				t.Errorf("want trigger %s, got %s", tc.want, got)
			}
		})
	}
}

func TestIncPodRestartMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cluster     string
		trigger     string
		wantType    string
		restartType RestartType
		wantSeries  int
	}{
		{
			name:        "planned warm restart",
			cluster:     "restart-planned-warm",
			restartType: quickRestart,
			trigger:     triggerPlanned,
			wantType:    restartTypeWarm,
			wantSeries:  1,
		},
		{
			name:        "planned cold restart",
			cluster:     "restart-planned-cold",
			restartType: podRestart,
			trigger:     triggerPlanned,
			wantType:    restartTypeCold,
			wantSeries:  1,
		},
		{
			name:        "upgrade cold restart",
			cluster:     "restart-upgrade-cold",
			restartType: podRestart,
			trigger:     triggerUpgrade,
			wantType:    restartTypeCold,
			wantSeries:  1,
		},
		{
			name:        "failure recovery warm restart",
			cluster:     "restart-failure-warm",
			restartType: quickRestart,
			trigger:     triggerFailureRecovery,
			wantType:    restartTypeWarm,
			wantSeries:  1,
		},
		{
			name:        "failure recovery cold restart",
			cluster:     "restart-failure-cold",
			restartType: podRestart,
			trigger:     triggerFailureRecovery,
			wantType:    restartTypeCold,
			wantSeries:  1,
		},
		{
			name:        "no restart is not counted",
			cluster:     "restart-none",
			restartType: noRestart,
			trigger:     triggerPlanned,
		},
		{
			name:        "conf update without restart is not counted",
			cluster:     "restart-conf-update",
			restartType: noRestartUpdateConf,
			trigger:     triggerPlanned,
		},
	}

	for idx := range tests {
		tc := tests[idx]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cluster := tc.cluster
			r := newMetricsTestReconciler(cluster)

			r.incPodRestartMetric(tc.restartType, tc.trigger)
			r.incPodRestartMetric(tc.restartType, tc.trigger)

			if got := clusterMetricSeries(t, podRestartsTotal, cluster); got != tc.wantSeries {
				t.Fatalf("want %d series, got %d", tc.wantSeries, got)
			}

			if tc.wantSeries == 0 {
				return
			}

			got := clusterMetricValue(t, podRestartsTotal, cluster, map[string]string{
				restartTypeLabelKey: tc.wantType,
				triggerLabelKey:     tc.trigger,
			})
			if got != 2 {
				t.Errorf("want count 2 for type=%s trigger=%s, got %v", tc.wantType, tc.trigger, got)
			}
		})
	}
}

func TestIncDynamicConfigUpdateMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cluster    string
		result     string
		wantResult string
	}{
		{
			name:       "success",
			cluster:    "dynamic-config-success",
			result:     dynamicConfigResultSuccess,
			wantResult: dynamicConfigResultSuccess,
		},
		{
			name:       "failed status",
			cluster:    "dynamic-config-failed",
			result:     dynamicConfigResultFromStatus(asdbv1.Failed),
			wantResult: dynamicConfigResultFailed,
		},
		{
			name:       "partially failed status",
			cluster:    "dynamic-config-partially-failed",
			result:     dynamicConfigResultFromStatus(asdbv1.PartiallyFailed),
			wantResult: dynamicConfigResultPartiallyFailed,
		},
	}

	for idx := range tests {
		tc := tests[idx]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cluster := tc.cluster
			r := newMetricsTestReconciler(cluster)

			r.incDynamicConfigUpdateMetric(tc.result)

			if got := clusterMetricSeries(t, dynamicConfigUpdatesTotal, cluster); got != 1 {
				t.Fatalf("want 1 series, got %d", got)
			}

			got := clusterMetricValue(t, dynamicConfigUpdatesTotal, cluster, map[string]string{
				resultLabelKey: tc.wantResult,
			})
			if got != 1 {
				t.Errorf("want count 1 for result=%s, got %v", tc.wantResult, got)
			}
		})
	}
}

func TestAddClusterPhaseMetricInitializesCounters(t *testing.T) {
	t.Parallel()

	const cluster = "counters-initialized"

	r := newMetricsTestReconciler(cluster)

	r.addClusterPhaseMetric()

	if got := clusterMetricSeries(t, podRestartsTotal, cluster); got != len(podRestartLabelValues) {
		t.Fatalf("want %d pod restart series, got %d", len(podRestartLabelValues), got)
	}

	for _, series := range gatherClusterSeries(t, podRestartsTotal, cluster) {
		if series.value != 0 {
			t.Errorf("want 0 for %v, got %v", series.labels, series.value)
		}

		if series.labels[restartTypeLabelKey] == restartTypeWarm && series.labels[triggerLabelKey] == triggerUpgrade {
			t.Errorf("unexpected warm upgrade series")
		}
	}

	if got := clusterMetricSeries(t, dynamicConfigUpdatesTotal, cluster); got != len(dynamicConfigResults) {
		t.Fatalf("want %d dynamic config update series, got %d", len(dynamicConfigResults), got)
	}

	// A later status write must not reset a counted restart.
	r.incPodRestartMetric(podRestart, triggerFailureRecovery)
	r.addClusterPhaseMetric()

	got := clusterMetricValue(t, podRestartsTotal, cluster, map[string]string{
		restartTypeLabelKey: restartTypeCold,
		triggerLabelKey:     triggerFailureRecovery,
	})
	if got != 1 {
		t.Errorf("want count 1 after a second addClusterPhaseMetric, got %v", got)
	}
}
