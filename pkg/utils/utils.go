package utils

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/ripemd160" //nolint:staticcheck,gosec // this ripemd160 legacy hash is only used for diff comparison not for security purpose
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	ls "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

const (
	DockerHubImagePrefix = "docker.io/"

	// ReasonImagePullBackOff when pod status is Pending as container image pull failed.
	ReasonImagePullBackOff = "ImagePullBackOff"
	// ReasonImageInspectError is error inspecting image.
	ReasonImageInspectError = "ImageInspectError"
	// ReasonErrImagePull is general image pull error.
	ReasonErrImagePull = "ErrImagePull"
	// ReasonRegistryUnavailable is the http error when pulling image from registry.
	ReasonRegistryUnavailable = "RegistryUnavailable"
)

// ClusterNamespacedName return namespaced name
func ClusterNamespacedName(aeroCluster *asdbv1.AerospikeCluster) string {
	return NamespacedName(aeroCluster.Namespace, aeroCluster.Name)
}

// NamespacedName return namespaced name
func NamespacedName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func GetNamespacedName(obj meta.Object) types.NamespacedName {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func GetNamespacedNameForSTSOrConfigMap(
	aeroCluster *asdbv1.AerospikeCluster, rackIdentifier string,
) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + rackIdentifier,
		Namespace: aeroCluster.Namespace,
	}
}

// IsImageEqual returns true if image name image1 is equal to image name image2.
func IsImageEqual(image1, image2 string) bool {
	desiredImageWithVersion := strings.TrimPrefix(image1, DockerHubImagePrefix)
	actualImageWithVersion := strings.TrimPrefix(image2, DockerHubImagePrefix)

	desiredRegistry, desiredName, desiredVersion := ParseDockerImageTag(desiredImageWithVersion)
	actualRegistry, actualName, actualVersion := ParseDockerImageTag(actualImageWithVersion)

	// registry name, image name and version should match.
	return desiredRegistry == actualRegistry && desiredName == actualName && (desiredVersion == actualVersion ||
		(desiredVersion == ":latest" && actualVersion == "") ||
		(actualVersion == ":latest" && desiredVersion == ""))
}

// ParseDockerImageTag parses input tag into registry, name and version.
func ParseDockerImageTag(tag string) (
	registry string, name string, version string,
) {
	if tag == "" {
		return "", "", ""
	}

	r := regexp.MustCompile(`(?P<registry>[^/]+/)?(?P<image>[^:]+)(?P<version>:.+)?`)
	matches := r.FindStringSubmatch(tag)

	return matches[1], matches[2], strings.TrimPrefix(matches[3], ":")
}

// IsPVCTerminating returns true if pvc's DeletionTimestamp has been set
func IsPVCTerminating(pvc *corev1.PersistentVolumeClaim) bool {
	return pvc.DeletionTimestamp != nil
}

// GetDesiredImage returns the desired image for the input containerName from the aeroCluster spec.
func GetDesiredImage(
	aeroCluster *asdbv1.AerospikeCluster, containerName string,
) (string, error) {
	if containerName == asdbv1.AerospikeServerContainerName {
		return aeroCluster.Spec.Image, nil
	}

	if containerName == asdbv1.AerospikeInitContainerName {
		return asdbv1.GetAerospikeInitContainerImage(aeroCluster), nil
	}

	sidecars := aeroCluster.Spec.PodSpec.Sidecars
	for idx := range sidecars {
		if sidecars[idx].Name == containerName {
			return sidecars[idx].Image, nil
		}
	}

	initSidecars := aeroCluster.Spec.PodSpec.InitContainers
	for idx := range initSidecars {
		if initSidecars[idx].Name == containerName {
			return initSidecars[idx].Image, nil
		}
	}

	return "", fmt.Errorf("container %s not found", containerName)
}

// LabelsForAerospikeCluster returns the labels for selecting the resources
// belonging to the given AerospikeCluster CR name.
func LabelsForAerospikeCluster(clName string) map[string]string {
	return map[string]string{
		asdbv1.AerospikeAppLabel:            "aerospike-cluster",
		asdbv1.AerospikeCustomResourceLabel: clName,
	}
}

// LabelsForAerospikeClusterRack returns the labels for specific rack
func LabelsForAerospikeClusterRack(
	clName string, rackID int, rackRevision string,
) map[string]string {
	labels := LabelsForAerospikeCluster(clName)
	labels[asdbv1.AerospikeRackIDLabel] = strconv.Itoa(rackID)

	if rackRevision != "" {
		labels[asdbv1.AerospikeRackRevisionLabel] = rackRevision
	}

	return labels
}

func GetAerospikeClusterRackLabelSelector(
	clName string, rackID int, rackRevision string,
) ls.Selector {
	labelSelector := ls.SelectorFromSet(LabelsForAerospikeClusterRack(clName, rackID, rackRevision))

	if rackRevision == "" {
		// rack-revision DoesNotExist
		reqRackRevision, _ := ls.NewRequirement(asdbv1.AerospikeRackRevisionLabel, selection.DoesNotExist, nil)

		labelSelector = labelSelector.Add(*reqRackRevision)
	}

	return labelSelector
}

// LabelsForPodAntiAffinity returns the labels to use for setting pod
// anti-affinity.
func LabelsForPodAntiAffinity(clName string) map[string]string {
	labels := LabelsForAerospikeCluster(clName)
	return labels
}

// LabelsForAerospikeBackupService returns the labels for selecting the resources
// belonging to the given AerospikeBackupService CR name.
func LabelsForAerospikeBackupService(clName string) map[string]string {
	return map[string]string{
		asdbv1.AerospikeAppLabel:            v1beta1.AerospikeBackupServiceKey,
		asdbv1.AerospikeCustomResourceLabel: clName,
	}
}

// MergeLabels merges operator an user defined labels
func MergeLabels(operatorLabels, userLabels map[string]string) map[string]string {
	mergedMap := make(map[string]string, len(operatorLabels)+len(userLabels))
	for label, value := range userLabels {
		mergedMap[label] = value
	}

	for label, value := range operatorLabels {
		mergedMap[label] = value
	}

	return mergedMap
}

// GetHash return ripemd160 hash for given string
func GetHash(str string) (string, error) {
	var digest []byte

	hash := ripemd160.New() //nolint:gosec // ripemd160 hash is only used for diff comparison not for security purpose
	hash.Reset()

	if _, err := hash.Write([]byte(str)); err != nil {
		return "", err
	}

	res := hash.Sum(digest)

	return hex.EncodeToString(res), nil
}

// GetRackIDAndRevisionFromSTSName gets rackID and rackRevision from the statefulset name.
// It assumes statefulset name is of format <cluster-name>-<rack-id> or <cluster-name>-<rack-id>-<rack-revision>
func GetRackIDAndRevisionFromSTSName(clusterName, statefulSetName string) (rackID int, rackRevision string, err error) {
	rackIdentifier := strings.TrimPrefix(statefulSetName, clusterName+"-")
	// Split the rackIdentifier into parts: rack-id and rack-revision (optional).
	parts := strings.SplitN(rackIdentifier, "-", 2)

	rackID, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf(
			"could not get rack id from rackIdentifier %s for sts %s: %v", rackIdentifier, statefulSetName, err)
	}

	if len(parts) > 1 {
		rackRevision = parts[1]
	}

	return rackID, rackRevision, nil
}

// ContainsString returns true if a string exists in a slice of strings.
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}

	return false
}

// RemoveString removes a string from a slice of strings.
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}

		result = append(result, item)
	}

	return
}

// Len32 returns length of slice in int32 range.
func Len32[T any](v []T) int32 {
	return int32(len(v)) //nolint:gosec // length can't exceed int32 range
}

func GetRackIdentifier(rackID int, rackRevision string) string {
	rackIDStr := strconv.Itoa(rackID)

	if rackRevision != "" {
		return fmt.Sprintf("%s-%s", rackIDStr, rackRevision)
	}

	return rackIDStr
}
