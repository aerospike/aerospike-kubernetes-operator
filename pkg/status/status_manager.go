package status

import "sync/atomic"

type Status uint32

const (
	Running Status = iota
	Succeeded
	Pending
)

type ReconciliationStatusManager struct {
	status uint32
}

var ReconciliationManager ReconciliationStatusManager

func (rsm *ReconciliationStatusManager) GetStatus() Status {
	switch atomic.LoadUint32(&rsm.status) {
	case 0:
		return Running
	case 1:
		return Succeeded
	default:
		return Pending
	}
}

func (rsm *ReconciliationStatusManager) SetStatus(status Status) {
	switch status {
	case Running:
		atomic.StoreUint32(&rsm.status, 0)
	case Succeeded:
		atomic.StoreUint32(&rsm.status, 1)
	default:
		atomic.StoreUint32(&rsm.status, 2)
	}
}

func NewReconciliationStatusManager() *ReconciliationStatusManager {
	reconciliationStatusManager := &ReconciliationStatusManager{}
	reconciliationStatusManager.SetStatus(Pending)
	return reconciliationStatusManager
}

func init() {
	ReconciliationManager = *NewReconciliationStatusManager()
}
