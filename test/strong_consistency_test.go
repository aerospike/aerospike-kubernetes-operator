package test

import (
	goctx "context"
	"fmt"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
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

		// Should we allow replication factor 1 in general or in SC mode
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

			aeroCluster.Spec.Size += 2
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, aeroNamespace)

			By("Set roster blacklist")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			aeroCluster.Spec.RosterBlacklist = []string{"2A1"}
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

			aeroCluster.Spec.Size -= 1
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

	// Check2 roster+blacklist >= len(pods)
	if len(rosterList)+len(aeroCluster.Spec.RosterBlacklist) < len(aeroCluster.Status.Pods) {
		err := fmt.Errorf("roster len not matching pods list. roster %v, blacklist %v, pods %v", rosterList, aeroCluster.Spec.RosterBlacklist, aeroCluster.Status.Pods)
		Expect(err).ToNot(HaveOccurred())
	}

	for _, roster := range rosterList {
		nodeID := strings.Split(roster, "@")[0]

		// Check1 roster should not have blacklisted pod
		contains := v1beta1.ContainsString(aeroCluster.Spec.RosterBlacklist, nodeID)
		Expect(contains).To(BeFalse(), "roster should not have blacklisted node", "roster", roster, "blacklist", aeroCluster.Spec.RosterBlacklist)

		// Check4 Scaledown: all the roster should be in pod list
		var found bool
		for _, pod := range aeroCluster.Status.Pods {
			if strings.EqualFold(nodeID, pod.Aerospike.NodeID) {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "roster should be in pod list", "roster", roster, "podlist", aeroCluster.Status.Pods)

	}

	// Check3 Scaleup: pod should be in roster or in blacklist
	for _, pod := range aeroCluster.Status.Pods {
		nodeID := pod.Aerospike.NodeID
		rackID := pod.Aerospike.RackID
		nodeRoster := nodeID + "@" + fmt.Sprint(rackID)

		if !v1beta1.ContainsString(aeroCluster.Spec.RosterBlacklist, nodeID) &&
			!v1beta1.ContainsString(rosterList, nodeRoster) {
			err := fmt.Errorf("pod not found in roster or blacklist. roster %v, blacklist %v, missing pod %v", rosterList, aeroCluster.Spec.RosterBlacklist, nodeRoster)
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

	return aerospikecluster.ParseInfoIntoMap(cmdOutput, ":", "=")
}
