package pkg

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/mitchellh/go-ps"
)

func (initp *InitParams) restartASD() error {
	data, err := os.ReadFile("/proc/1/cmdline")
	if err != nil {
		return err
	}

	strData := string(data)
	// Check if we are running under tini to be able to warm restart.
	if !(strings.Contains(strData, "tini") && strings.Contains(strData, "-r")) {
		return fmt.Errorf("warm restart not supported - aborting")
	}

	// Create new Aerospike configuration
	if err := initp.copyTemplates(configMapDir, configVolume); err != nil {
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

	// Get current asd pid
	for _, proc := range processes {
		if proc.Executable() == "asd" {
			asdPid = proc.Pid()
		}
	}

	if asdPid == -1 {
		return fmt.Errorf("error getting Aerospike server PID")
	}

	// Restart ASD by signalling the init process
	if err := syscall.Kill(1, syscall.SIGUSR1); err != nil {
		return err
	}

	// Wait for asd process to restart.
	for i := 1; i <= 60; i++ {
		asdProc, psErr := ps.FindProcess(asdPid)
		if psErr != nil {
			return psErr
		}

		if asdProc != nil {
			initp.logger.Info("Waiting for Aerospike server to terminate ...")
			time.Sleep(5 * time.Second)
		} else {
			isTerminated = true
			break
		}
	}

	if !isTerminated {
		// ASD did not terminate within stipulated time.
		return fmt.Errorf("aborting warm start - Aerospike server did not terminate")
	}

	initp.logger.Info("Successfully terminated Aerospike server during warm restart")

	return nil
}
