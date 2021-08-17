package utils

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"golang.org/x/crypto/ripemd160"
	corev1 "k8s.io/api/core/v1"
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

	aerospikeConfConfigMapPrefix = "aerospike-conf"
)

// ClusterNamespacedName return namespaced name
func ClusterNamespacedName(aeroCluster *asdbv1beta1.AerospikeCluster) string {
	return NamespacedName(aeroCluster.Namespace, aeroCluster.Name)
}

// ConfigMapName returns the name to use for a aeroCluster cluster's config map.
func ConfigMapName(aeroCluster *asdbv1beta1.AerospikeCluster) string {
	return fmt.Sprintf("%s-%s", aerospikeConfConfigMapPrefix, aeroCluster.Name)
}

// NamespacedName return namespaced name
func NamespacedName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// IsImageEqual returns true if image name image1 is equal to image name image2.
func IsImageEqual(image1 string, image2 string) bool {
	desiredImageWithVersion := strings.TrimPrefix(image1, DockerHubImagePrefix)
	actualImageWithVersion := strings.TrimPrefix(image2, DockerHubImagePrefix)

	desiredRegistry, desiredName, desiredVersion := ParseDockerImageTag(desiredImageWithVersion)
	actualRegistry, actualName, actualVersion := ParseDockerImageTag(actualImageWithVersion)

	// registry name, image name and version should match.
	return desiredRegistry == actualRegistry && desiredName == actualName && (desiredVersion == actualVersion || (desiredVersion == ":latest" && actualVersion == "") || (actualVersion == ":latest" && desiredVersion == ""))
}

// ParseDockerImageTag parses input tag into registry, name and version.
func ParseDockerImageTag(tag string) (registry string, name string, version string) {
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
func GetDesiredImage(aeroCluster *asdbv1beta1.AerospikeCluster, containerName string) (string, error) {
	if containerName == asdbv1beta1.AerospikeServerContainerName {
		return aeroCluster.Spec.Image, nil
	}

	for _, sidecar := range aeroCluster.Spec.PodSpec.Sidecars {
		if sidecar.Name == containerName {
			return sidecar.Image, nil
		}
	}

	return "", fmt.Errorf("Container %s not found", containerName)
}

// LabelsForAerospikeCluster returns the labels for selecting the resources
// belonging to the given AerospikeCluster CR name.
func LabelsForAerospikeCluster(clName string) map[string]string {
	return map[string]string{"app": "aerospike-cluster", "aerospike.com/cr": clName}
}

// LabelsForAerospikeClusterRack returns the labels for specific rack
func LabelsForAerospikeClusterRack(clName string, rackID int) map[string]string {
	labels := LabelsForAerospikeCluster(clName)
	labels["aerospike.com/rack-id"] = strconv.Itoa(rackID)
	return labels
}

// GetHash return ripmd160 hash for given string
func GetHash(str string) (string, error) {
	var digest []byte
	hash := ripemd160.New()
	hash.Reset()
	if _, err := hash.Write([]byte(str)); err != nil {
		return "", err
	}
	res := hash.Sum(digest)
	return hex.EncodeToString(res), nil
}

func GetRackIDFromSTSName(statefulSetName string) (*int, error) {
	parts := strings.Split(statefulSetName, "-")
	if len(parts) < 2 {
		return nil, fmt.Errorf("failed to get rackID from statefulSetName %s", statefulSetName)
	}
	// stsname ==> clustername-rackid
	rackStr := parts[len(parts)-1]
	rackID, err := strconv.Atoi(rackStr)
	if err != nil {
		return nil, err
	}
	return &rackID, nil
}

// Helper functions to check and remove string from a slice of strings.
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func TruncateString(str string, num int) string {
	if len(str) > num {
		return str[0:num]
	}
	return str
}
