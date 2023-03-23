package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mitchellh/go-ps"
	"github.com/sirupsen/logrus"
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

	if err := initp.createAerospikeConf(); err != nil {
		return err
	}

	processes, psErr := ps.Processes()
	if psErr != nil {
		return psErr
	}

	asdPid := -1
	isTerminated := false

	for _, proc := range processes {
		if proc.Executable() == "asd" {
			asdPid = proc.Pid()
		}
	}

	if asdPid == -1 {
		return fmt.Errorf("error getting Aerospike server PID")
	}

	if err := syscall.Kill(1, syscall.SIGUSR1); err != nil {
		return err
	}

	for i := 1; i <= 60; i++ {
		asdProc, psErr := ps.FindProcess(asdPid)
		if psErr != nil {
			return psErr
		}

		if asdProc != nil {
			logrus.Info("Waiting for Aerospike server to terminate ...")
			time.Sleep(5 * time.Second)
		} else {
			isTerminated = true
			break
		}
	}

	if !isTerminated {
		return fmt.Errorf("aborting warm start - Aerospike server did not terminate")
	}

	return nil
}
