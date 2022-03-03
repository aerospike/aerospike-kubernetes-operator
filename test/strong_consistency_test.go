package test

import (
	goctx "context"
	"fmt"
	"strings"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v5"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("SCMode", func() {
	Context("When doing valid operation", func() {
		// What do you know
		// Check for roster after all these operations
		// Deploy
		// Add node
		// Remove node

		// Dead/Unavailable partition
		// If there are D/U p then it should stuck and not succeed,
		// Currently status updated before roster. Thus, success would mean setting roster
		// Also, when D/U p are fixed, it should succeed

		// Rack aware setup
		ctx := goctx.TODO()

		clusterName := "scmode"
		clusterNamespacedName := getClusterNamespacedName(
			clusterName, namespace,
		)

		It("Should test sc cluster lifecycle", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig()
			aeroNamespace := "test"
			racks := []asdbv1beta1.Rack{
				{ID: 1},
				{ID: 2},
			}
			rackConf := asdbv1beta1.RackConfig{
				Namespaces: []string{aeroNamespace},
				Racks:      racks,
			}
			aeroCluster.Spec.RackConfig = rackConf

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, aeroNamespace)

			By("Add node")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size += 3
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, aeroNamespace)

			By("Remove node")
			// Remove 1 node
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 1
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, aeroNamespace)

			// Remove 2 node
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 2
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, aeroNamespace)
		})

	})
	// Context("When doing invalid operation", func() {
	// 	// Validation: can not remove more than replica node.
	// 	//             not allow updating strong-consistency config
	// 	It("Should not allow updating strong-consistency config", func() {

	// 	})
	// })

})

// roster: node-id@rack-id

func validateRoster(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, aeroNamespace string) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	hostConns, err := newAllHostConn(logger, aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())

	rosterNodesMap, err := getRoster(hostConns[0], getClientPolicy(aeroCluster, k8sClient), aeroNamespace)
	Expect(err).ToNot(HaveOccurred())

	rosterStr := rosterNodesMap["roster"]
	rosterList := strings.Split(rosterStr, ",")
	if len(rosterList) != len(aeroCluster.Status.Pods) {
		err := fmt.Errorf("roster len not matching pods list. roster %v, pods %v", rosterNodesMap, aeroCluster.Status.Pods)
		Expect(err).ToNot(HaveOccurred())
	}

	for _, pod := range aeroCluster.Status.Pods {
		nodeID := pod.Aerospike.NodeID
		rackID := pod.Aerospike.RackID
		nodeRoster := nodeID + "@" + fmt.Sprint(rackID)
		if !strings.Contains(strings.ToLower(rosterStr), strings.ToLower(nodeRoster)) {
			err := fmt.Errorf("roster not matching pod list. roster %v, missing pod %v", rosterNodesMap, nodeRoster)
			Expect(err).ToNot(HaveOccurred())
		}
	}
}

func getRoster(hostConn *deployment.HostConn, aerospikePolicy *as.ClientPolicy, namespace string) (map[string]string, error) {
	cmd := fmt.Sprintf("roster:namespace=%s", namespace)
	res, err := hostConn.ASConn.RunInfo(aerospikePolicy, cmd)
	if err != nil {
		return nil, err
	}

	cmdOutput := res[cmd]

	// r.Log.V(1).Info("Run info command", "host", hostConn.String(), "cmd", cmd, "output", cmdOutput)

	return aerospikecluster.ParseInfoIntoMap(cmdOutput, ":", "=")

}
