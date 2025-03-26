package v1

import v1 "k8s.io/api/core/v1"

const (
	baseVersion                  = "6.0.0.0"
	baseInitVersion              = "1.0.0"
	minInitVersionForDynamicConf = "2.2.0"
)

func getContainerNames(containers []v1.Container) []string {
	containerNames := make([]string, 0, len(containers))

	for idx := range containers {
		containerNames = append(containerNames, containers[idx].Name)
	}

	return containerNames
}
