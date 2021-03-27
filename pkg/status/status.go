package status

import (
	"context"
	"github.com/go-logr/logr"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type BackupStatusUpdater struct {
	Manager manager.Manager

	requestCh chan backupStatusUpdateRequest

	logger logr.Logger
}

type backupStatusRequestType int

const (
	InProgress backupStatusRequestType = iota
	Finished  backupStatusRequestType = iota
	Failed    backupStatusRequestType = iota
)

type backupStatusUpdateRequest struct {
	backup *api.CassandraBackup

	requestType backupStatusRequestType

	podName string
}

func NewBackupStatusUpdater(mgr manager.Manager) *BackupStatusUpdater {
	return &BackupStatusUpdater{
		Manager: mgr,
		requestCh: make(chan backupStatusUpdateRequest),
		logger: mgr.GetLogger().WithName("backup-status-updater"),
	}
}

func (updater *BackupStatusUpdater) Start(stopCh <-chan struct{}) error {
	updater.logger.Info("starting")

Loop:
	for {
		select {
		case req := <-updater.requestCh:
			return updater.updateStatus(req)
		case <-stopCh:
			break Loop
		}
	}
	updater.shutdown()
	return nil
}

func (updater *BackupStatusUpdater) run(stopCh <-chan struct{}) {
	Loop:
		for {
			select {
			case req := <-updater.requestCh:
				updater.updateStatus(req)
			case <-stopCh:
				break Loop
			}
		}
	updater.shutdown()
}

func (updater *BackupStatusUpdater) SetInProgress(backup *api.CassandraBackup, podName string) {
	request := backupStatusUpdateRequest{
		backup:      backup,
		requestType: InProgress,
		podName:     podName,
	}
	updater.requestCh <- request
}

func (updater *BackupStatusUpdater) SetFinished(backup *api.CassandraBackup, podName string) {
	request := backupStatusUpdateRequest{
		backup:      backup,
		requestType: Finished,
		podName:     podName,
	}
	updater.requestCh <- request
}

func (updater *BackupStatusUpdater) SetFailed(backup *api.CassandraBackup, podName string) {
	request := backupStatusUpdateRequest{
		backup:      backup,
		requestType: Failed,
		podName:     podName,
	}
	updater.requestCh <- request
}

func (updater *BackupStatusUpdater) updateStatus(req backupStatusUpdateRequest) error {
	updater.logger.Info("updating status",
		"Backup", types.NamespacedName{Namespace: req.backup.Namespace, Name: req.backup.Name},
		"UpdateType", req.requestType)

	client := updater.Manager.GetClient()
	backup := req.backup.DeepCopy()
	patch := ctrlclient.MergeFromWithOptions(backup.DeepCopy(), ctrlclient.MergeFromWithOptimisticLock{})

	switch req.requestType {
	case InProgress:
		//patch := ctrlclient.MergeFromWithOptions(req.backup.DeepCopy(), ctrlclient.MergeFromWithOptimisticLock{})
	case Finished:
		backup.Status.InProgress = removeValue(backup.Status.InProgress, req.podName)
		backup.Status.Finished = append(backup.Status.Finished, req.podName)
	case Failed:
		backup.Status.InProgress = removeValue(backup.Status.InProgress, req.podName)
		backup.Status.Failed = append(backup.Status.Failed, req.podName)
	}

	if patch != nil {
		if err := client.Status().Patch(context.Background(), backup, patch); err != nil {
			updater.logger.Error(err,"failed to update status",
				"Backup", types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name},
				"UpdateType", req.requestType)
			return err
		}
	}
	return nil
}

func (updater *BackupStatusUpdater) shutdown() {
	updater.logger.Info("shutting down")
	close(updater.requestCh)
}

func removeValue(slice []string, value string) []string {
	newSlice := make([]string, 0)
	for _, s := range slice {
		if s != value {
			newSlice = append(newSlice, s)
		}
	}
	return newSlice
}
