package v1

import (
	"fmt"
	"path/filepath"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// validateStorageSpecChange indicates if a change to storage spec is safe to apply.
func validateStorageSpecChange(oldStorage, newStorage *asdbv1.AerospikeStorageSpec) error {
	for newVolIdx := range newStorage.Volumes {
		newVolume := &newStorage.Volumes[newVolIdx]

		for oldVolIdx := range oldStorage.Volumes {
			oldVolume := &oldStorage.Volumes[oldVolIdx]
			if oldVolume.Name == newVolume.Name {
				if !isSafeChange(oldVolume, newVolume) {
					// Validate same volumes
					return fmt.Errorf(
						"cannot change volumes old: %v newStorage %v", oldVolume,
						newVolume,
					)
				}

				break
			}
		}
	}

	_, _, err := validateAddedOrRemovedVolumes(oldStorage, newStorage)

	return err
}

// validateAddedOrRemovedVolumes returns volumes that were added or removed.
// Currently, only configMap volume is allowed to be added or removed dynamically
func validateAddedOrRemovedVolumes(oldStorage, newStorage *asdbv1.AerospikeStorageSpec) (
	addedVolumes []asdbv1.VolumeSpec, removedVolumes []asdbv1.VolumeSpec, err error,
) {
	// Validate Persistent volume
	for newVolIdx := range newStorage.Volumes {
		newVolume := &newStorage.Volumes[newVolIdx]
		matched := false

		for oldVolIdx := range oldStorage.Volumes {
			oldVolume := &oldStorage.Volumes[oldVolIdx]
			if oldVolume.Name == newVolume.Name {
				matched = true
				break
			}
		}

		if !matched {
			if newVolume.Source.PersistentVolume != nil {
				return []asdbv1.VolumeSpec{}, []asdbv1.VolumeSpec{}, fmt.Errorf(
					"cannot add persistent volume: %v", newVolume,
				)
			}

			addedVolumes = append(addedVolumes, *newVolume)
		}
	}

	for oldVolIdx := range oldStorage.Volumes {
		oldVolume := &oldStorage.Volumes[oldVolIdx]
		matched := false

		for newVolIdx := range newStorage.Volumes {
			newVolume := &newStorage.Volumes[newVolIdx]
			if oldVolume.Name == newVolume.Name {
				matched = true
			}
		}

		if !matched {
			if oldVolume.Source.PersistentVolume != nil {
				return []asdbv1.VolumeSpec{}, []asdbv1.VolumeSpec{}, fmt.Errorf(
					"cannot remove persistent volume: %v", oldVolume,
				)
			}

			removedVolumes = append(removedVolumes, *oldVolume)
		}
	}

	return addedVolumes, removedVolumes, nil
}

// setStorageDefaults sets default values for storage spec fields.
func setStorageDefaults(storage *asdbv1.AerospikeStorageSpec) {
	defaultFilesystemInitMethod := asdbv1.AerospikeVolumeMethodNone
	defaultFilesystemWipeMethod := asdbv1.AerospikeVolumeMethodDeleteFiles
	defaultBlockInitMethod := asdbv1.AerospikeVolumeMethodNone
	defaultBlockWipeMethod := asdbv1.AerospikeVolumeMethodDD
	defaultCleanupThreads := asdbv1.AerospikeVolumeSingleCleanupThread
	// Set storage level defaults.
	setAerospikePersistentVolumePolicyDefaults(
		&storage.FileSystemVolumePolicy,
		&asdbv1.AerospikePersistentVolumePolicySpec{
			InitMethod: defaultFilesystemInitMethod, WipeMethod: defaultFilesystemWipeMethod, CascadeDelete: false,
		},
	)

	setAerospikePersistentVolumePolicyDefaults(
		&storage.BlockVolumePolicy,
		&asdbv1.AerospikePersistentVolumePolicySpec{
			InitMethod: defaultBlockInitMethod, WipeMethod: defaultBlockWipeMethod, CascadeDelete: false,
		},
	)

	if storage.CleanupThreads == 0 {
		storage.CleanupThreads = defaultCleanupThreads
	}

	for idx := range storage.Volumes {
		switch {
		case storage.Volumes[idx].Source.PersistentVolume == nil:
			// All other sources are considered to be mounted, for now.
			setAerospikePersistentVolumePolicyDefaults(&storage.Volumes[idx].AerospikePersistentVolumePolicySpec,
				&storage.FileSystemVolumePolicy)
		case storage.Volumes[idx].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock:
			setAerospikePersistentVolumePolicyDefaults(&storage.Volumes[idx].AerospikePersistentVolumePolicySpec,
				&storage.BlockVolumePolicy)
		case storage.Volumes[idx].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem:
			setAerospikePersistentVolumePolicyDefaults(&storage.Volumes[idx].AerospikePersistentVolumePolicySpec,
				&storage.FileSystemVolumePolicy)
		}
	}
}

func validateHostPathVolumeReadOnly(volume *asdbv1.VolumeSpec) error {
	if volume.Source.HostPath != nil {
		var attachments []asdbv1.VolumeAttachment
		attachments = append(attachments, volume.Sidecars...)
		attachments = append(attachments, volume.InitContainers...)

		for idx := range attachments {
			if !asdbv1.GetBool(attachments[idx].ReadOnly) {
				return fmt.Errorf("hostpath volume %s can only be mounted as read-only filesystem in %s container",
					volume.Name, attachments[idx].ContainerName)
			}
		}

		if volume.Aerospike != nil {
			if !asdbv1.GetBool(volume.Aerospike.ReadOnly) {
				return fmt.Errorf("hostpath volume %s can only be mounted as read-only filesystem in server container",
					volume.Name)
			}
		}
	}

	return nil
}

// setAerospikePersistentVolumePolicyDefaults applies default values to unset fields of the policy using corresponding
// fields from defaultPolicy
func setAerospikePersistentVolumePolicyDefaults(pvPolicy *asdbv1.AerospikePersistentVolumePolicySpec,
	defaultPolicy *asdbv1.AerospikePersistentVolumePolicySpec) {
	if pvPolicy.InputInitMethod == nil {
		pvPolicy.InitMethod = defaultPolicy.InitMethod
	} else {
		pvPolicy.InitMethod = *pvPolicy.InputInitMethod
	}

	if pvPolicy.InputWipeMethod == nil {
		pvPolicy.WipeMethod = defaultPolicy.WipeMethod
	} else {
		pvPolicy.WipeMethod = *pvPolicy.InputWipeMethod
	}

	if pvPolicy.InputCascadeDelete == nil {
		pvPolicy.CascadeDelete = defaultPolicy.CascadeDelete
	} else {
		pvPolicy.CascadeDelete = *pvPolicy.InputCascadeDelete
	}
}

// GetPVsVolumesFromStorage returns the PV volumes from the storage spec.
func GetPVsVolumesFromStorage(storage *asdbv1.AerospikeStorageSpec) (pVs []asdbv1.VolumeSpec) {
	for idx := range storage.Volumes {
		volume := storage.Volumes[idx]
		if volume.Source.PersistentVolume != nil {
			pVs = append(pVs, volume)
		}
	}

	return pVs
}

// GetNonPVsVolumesFromStorage returns the non PV volumes from the storage spec.
func GetNonPVsVolumesFromStorage(storage *asdbv1.AerospikeStorageSpec) (nonPVs []asdbv1.VolumeSpec) {
	for idx := range storage.Volumes {
		volume := storage.Volumes[idx]
		if volume.Source.PersistentVolume == nil {
			nonPVs = append(nonPVs, volume)
		}
	}

	return nonPVs
}

// getAerospikeStorageList gives blockStorageDeviceList and fileStorageList
func getAerospikeStorageList(storage *asdbv1.AerospikeStorageSpec, onlyPV bool) (
	blockStorageDeviceList []string, fileStorageList []string, err error,
) {
	for idx := range storage.Volumes {
		volume := &storage.Volumes[idx]
		if volume.Aerospike != nil {
			if volume.Source.PersistentVolume == nil {
				if !onlyPV {
					fileStorageList = append(fileStorageList, volume.Aerospike.Path)
				}

				continue
			}

			switch {
			case volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock:
				blockStorageDeviceList = append(blockStorageDeviceList, volume.Aerospike.Path)
			case volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem:
				fileStorageList = append(fileStorageList, volume.Aerospike.Path)
			default:
				return nil, nil, fmt.Errorf(
					"invalid volumemode %s, valid volumemods %s, %s",
					volume.Source.PersistentVolume.VolumeMode,
					v1.PersistentVolumeBlock, v1.PersistentVolumeFilesystem,
				)
			}
		}
	}

	return blockStorageDeviceList, fileStorageList, nil
}

func validateStorage(
	storage *asdbv1.AerospikeStorageSpec, podSpec *asdbv1.AerospikePodSpec,
) error {
	if asdbv1.GetBool(storage.DeleteLocalStorageOnRestart) && len(storage.LocalStorageClasses) == 0 {
		return fmt.Errorf(
			"localStorageClasses cannot be empty if deleteLocalStorageOnRestart is set",
		)
	}

	reservedPaths := map[string]int{
		// Reserved mount paths for the operator.
		"/etc/aerospike": 1,
		"/configs":       1,
	}

	storagePaths := map[string]int{}

	sidecarAttachmentPaths := map[string]map[string]int{}
	initContainerAttachmentPaths := map[string]map[string]int{}

	for idx := range storage.Volumes {
		volume := &storage.Volumes[idx]

		errors := validation.IsDNS1123Label(volume.Name)
		if len(errors) > 0 {
			return fmt.Errorf(
				"invalid volume name '%s' - %v", volume.Name,
				errors,
			)
		}

		if err := validateStorageVolumeSource(volume); err != nil {
			return fmt.Errorf(
				"invalid volumeSource: %v, volume %v", err, volume,
			)
		}

		if volume.Aerospike == nil && len(volume.Sidecars) == 0 && len(volume.InitContainers) == 0 {
			return fmt.Errorf(
				"invalid volume, no container provided to attach the volume. "+
					"At least one of the following must be provided: volume.Aerospike, volume.Sidecars, "+
					"volume.InitContainers, volume %v",
				volume,
			)
		}

		// Validate Aerospike attachment
		if volume.Aerospike != nil {
			if !filepath.IsAbs(volume.Aerospike.Path) {
				return fmt.Errorf(
					"volume path should be absolute: %s", volume.Aerospike.Path,
				)
			}

			if _, ok := reservedPaths[volume.Aerospike.Path]; ok {
				return fmt.Errorf(
					"reserved volume path %s", volume.Aerospike.Path,
				)
			}

			if _, ok := storagePaths[volume.Aerospike.Path]; ok {
				return fmt.Errorf(
					"duplicate volume path %s", volume.Aerospike.Path,
				)
			}

			storagePaths[volume.Aerospike.Path] = 1
		}

		// Validate sidecars attachments
		if err := validateAttachment(
			volume.Sidecars, getContainerNames(podSpec.Sidecars),
		); err != nil {
			return fmt.Errorf("sidecar volumeMount validation failed, %v", err)
		}

		// Validate initContainer attachments
		if err := validateAttachment(
			volume.InitContainers, getContainerNames(podSpec.InitContainers),
		); err != nil {
			return fmt.Errorf(
				"initContainer volumeMount validation failed, %v", err,
			)
		}

		// Validate attachments path
		if err := validateContainerAttachmentPaths(
			volume.Sidecars, sidecarAttachmentPaths,
		); err != nil {
			return err
		}

		if err := validateContainerAttachmentPaths(
			volume.InitContainers, initContainerAttachmentPaths,
		); err != nil {
			return err
		}

		if err := validateHostPathVolumeReadOnly(volume); err != nil {
			return err
		}
	}

	return nil
}

func validateContainerAttachmentPaths(
	volumeAttachments []asdbv1.VolumeAttachment,
	containerAttachmentPaths map[string]map[string]int,
) error {
	for _, attachment := range volumeAttachments {
		if _, ok := containerAttachmentPaths[attachment.ContainerName]; !ok {
			containerAttachmentPaths[attachment.ContainerName] = map[string]int{attachment.Path: 1}
			continue
		}

		if _, ok := containerAttachmentPaths[attachment.ContainerName][attachment.Path]; ok {
			return fmt.Errorf(
				"duplicate path %s for volumeAttachments %v in container %s",
				attachment.Path, volumeAttachments, attachment.ContainerName,
			)
		}

		containerAttachmentPaths[attachment.ContainerName][attachment.Path] = 1
	}

	return nil
}

func validateAttachment(
	volumeAttachments []asdbv1.VolumeAttachment, containerNames []string,
) error {
	attachmentContainers := map[string]int{}

	for _, attachment := range volumeAttachments {
		if attachment.ContainerName == asdbv1.AerospikeInitContainerName {
			return fmt.Errorf(
				"cannot attach volumes to: %s",
				asdbv1.AerospikeInitContainerName,
			)
		}

		if attachment.ContainerName == asdbv1.AerospikeServerContainerName {
			return fmt.Errorf(
				"use storage.aerospike for attaching volumes to Aerospike server container %s for path %s",
				asdbv1.AerospikeServerContainerName, attachment.Path,
			)
		}

		if !filepath.IsAbs(attachment.Path) {
			return fmt.Errorf(
				"volume path should be absolute: %s", attachment.Path,
			)
		}

		// Check for duplicate container entries
		if _, ok := attachmentContainers[attachment.ContainerName]; ok {
			return fmt.Errorf(
				"duplicate container name %s in volumeAttachments %v",
				attachment.ContainerName, volumeAttachments,
			)
		}

		attachmentContainers[attachment.ContainerName] = 1

		// Check container name, should be valid.
		var found bool

		for _, name := range containerNames {
			if attachment.ContainerName == name {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf(
				"volumeMount containerName `%s` not found in container names %v",
				attachment.ContainerName, containerNames,
			)
		}
	}

	return nil
}

// isSafeChange indicates if a change to a volume is safe to allow.
func isSafeChange(oldVolume, newVolume *asdbv1.VolumeSpec) bool {
	// Allow all type of sources, except pv
	if oldVolume.Source.PersistentVolume == nil && newVolume.Source.PersistentVolume == nil {
		return true
	}

	if (oldVolume.Source.PersistentVolume != nil && newVolume.Source.PersistentVolume == nil) ||
		(oldVolume.Source.PersistentVolume == nil && newVolume.Source.PersistentVolume != nil) {
		return false
	}

	oldPV := oldVolume.Source.PersistentVolume
	newPV := newVolume.Source.PersistentVolume

	return reflect.DeepEqual(oldPV, newPV)
}

func validateStorageVolumeSource(volume *asdbv1.VolumeSpec) error {
	source := volume.Source
	sourceCount := 0

	if source.EmptyDir != nil {
		sourceCount++
	}

	if source.Secret != nil {
		sourceCount++
	}

	if source.ConfigMap != nil {
		sourceCount++
	}

	if source.PersistentVolume != nil {
		sourceCount++
	}

	if source.HostPath != nil {
		sourceCount++
	}

	if sourceCount == 0 {
		return fmt.Errorf("no volume source found")
	}

	if sourceCount > 1 {
		return fmt.Errorf("can not specify more than 1 source")
	}

	if source.PersistentVolume != nil {
		// Validate VolumeMode
		vm := source.PersistentVolume.VolumeMode

		// Validate InitMethod
		if vm == v1.PersistentVolumeBlock {
			// Validate the initialization method for the volume
			validInitMethods := sets.New(
				asdbv1.AerospikeVolumeMethodDD,
				asdbv1.AerospikeVolumeMethodHeaderCleanup,
				asdbv1.AerospikeVolumeMethodBlkdiscard,
				asdbv1.AerospikeVolumeMethodNone,
				asdbv1.AerospikeVolumeMethodBlkdiscardWithHeaderCleanup,
			)

			if !validInitMethods.Has(volume.InitMethod) {
				return fmt.Errorf(
					"invalid init method %v for block volume: %v",
					volume.InitMethod, volume,
				)
			}

			// Validate the wipe method for the volume
			validWipeMethods := sets.New(
				asdbv1.AerospikeVolumeMethodBlkdiscard,
				asdbv1.AerospikeVolumeMethodDD,
			)

			if !validWipeMethods.Has(volume.WipeMethod) {
				return fmt.Errorf(
					"invalid wipe method %s for block volume: %s",
					volume.WipeMethod, volume.Name,
				)
			}
		} else {
			validInitMethods := sets.New(asdbv1.AerospikeVolumeMethodNone, asdbv1.AerospikeVolumeMethodDeleteFiles)
			if !validInitMethods.Has(volume.InitMethod) {
				return fmt.Errorf(
					"invalid init method %v for filesystem volume: %v",
					volume.InitMethod, volume,
				)
			}

			validWipeMethods := sets.New(asdbv1.AerospikeVolumeMethodDeleteFiles)
			if !validWipeMethods.Has(volume.WipeMethod) {
				return fmt.Errorf(
					"invalid wipe method %s for filesystem volume: %s",
					volume.WipeMethod, volume.Name,
				)
			}
		}
	}

	return nil
}
