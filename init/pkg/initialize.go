package pkg

import (
	goctx "context"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	configVolume = "/etc/aerospike"
	configMapDir = "/etc/aerospike/configmap"
)

// ColdRestart initializes storage devices on first pod run.
func (initp *InitParams) ColdRestart(ctx goctx.Context) error {
	// Create required directories.
	if err := initp.makeWorkDir(); err != nil {
		return err
	}

	// Copy required files to config volume for initialization.
	if err := initp.copyTemplates("/configs", configVolume); err != nil {
		return err
	}

	// Copy scripts and binaries needed for warm restart.
	// Init should not fail if features.conf file is not present
	// akoinit binary will always be present here, as same has been checked in entrypoint.sh
	filesToCopy := [2]string{"/workdir/bin/akoinit", "/configs/features.conf"}
	for _, file := range filesToCopy {
		if _, err := os.Stat(file); err == nil {
			cmd := exec.Command("cp", "--dereference", file, configVolume)
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}

	if err := os.MkdirAll(configMapDir, 0755); err != nil { //nolint:gocritic // file permission
		return err
	}

	matches, err := filepath.Glob("/configs/*")
	if err != nil {
		return err
	}

	for _, path := range matches {
		cmd := exec.Command("cp", "--dereference", "--recursive", path, configMapDir)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	initp.logger.Info("Copied all files from configmap to configmap directory",
		"source", "/configs", "destination", configMapDir)

	if err := initp.createAerospikeConf(); err != nil {
		return err
	}

	if err := initp.manageVolumesAndUpdateStatus(ctx, "podRestart"); err != nil {
		return err
	}

	return nil
}
