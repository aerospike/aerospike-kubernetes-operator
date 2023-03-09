package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (initp *InitParams) restartASD() error {
	data, err := os.ReadFile("/proc/1/cmdline")
	if err != nil {
		return err
	}

	strdata := string(data)
	if !(strings.Contains(strdata, "tini") && strings.Contains(strdata, "-r")) {
		return fmt.Errorf("warm restart not supported - aborting")
	}

	// Create new Aerospike configuration
	if err := initp.copyTemplates(filepath.Join(configVolume, "configmap"), configVolume); err != nil {
		return err
	}

	return nil
}
