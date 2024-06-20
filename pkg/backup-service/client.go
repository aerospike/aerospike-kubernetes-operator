package backupservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-logr/logr"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

const restAPIVersion = "v1"
const defaultContextPath = "/"

type Client struct {
	// The address to listen on.
	Address *string `json:"address,omitempty"`
	// The port to listen on.
	Port *int `json:"port,omitempty"`
	// ContextPath customizes path for the API endpoints.
	ContextPath *string `json:"context-path,omitempty"`
}

func GetBackupServiceClient(config *v1beta1.ServiceConfig) *Client {
	return &Client{
		Address:     config.Address,
		Port:        config.Port,
		ContextPath: config.ContextPath,
	}
}

func (b *Client) GetAddress() *string {
	return b.Address
}

func (b *Client) GetPort() *int {
	return b.Port
}

func (b *Client) GetContextPath() string {
	if b.ContextPath != nil {
		return *b.ContextPath
	}

	return defaultContextPath
}

func (b *Client) CheckBackupServiceHealth() error {
	url := b.API("/health")

	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("backup service is not healthy")
	}

	return nil
}

func (b *Client) GetBackupServiceConfig() (*model.Config, error) {
	url := b.API("/config")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup service config")
	}

	var conf model.Config

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &conf); err != nil {
		return nil, err
	}

	return &conf, nil
}

func (b *Client) ApplyConfig() error {
	url := b.API("/config/apply")

	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to apply latest config, error: %s", string(body))
	}

	return nil
}

func (b *Client) GetClusters() (map[string]*model.AerospikeCluster, error) {
	url := b.API("/config/clusters")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get aerospike clusters")
	}

	aerospikeClusters := make(map[string]*model.AerospikeCluster)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &aerospikeClusters); err != nil {
		return nil, err
	}

	return aerospikeClusters, nil
}

func (b *Client) PutCluster(cluster *v1beta1.Cluster) error {
	url := b.API(fmt.Sprintf("/config/clusters/%s", cluster.Name))

	jsonBody, err := json.Marshal(cluster.AerospikeCluster)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put aerospike cluster, error: %s", string(body))
	}

	return nil
}

func (b *Client) UpdateCluster(cluster *v1beta1.Cluster) error {
	url := b.API(fmt.Sprintf("/config/clusters/%s", cluster.Name))

	jsonBody, err := json.Marshal(cluster.AerospikeCluster)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update aerospike cluster, error: %s", string(body))
	}

	return nil
}

func (b *Client) GetBackupPolicies() (map[string]*model.BackupPolicy, error) {
	url := b.API("/config/policies")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup policies")
	}

	policies := make(map[string]*model.BackupPolicy)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &policies); err != nil {
		return nil, err
	}

	return policies, nil
}

func (b *Client) PutBackupPolicy(name string, policy *model.BackupPolicy) error {
	url := b.API(fmt.Sprintf("/config/policies/%s", name))

	jsonBody, err := json.Marshal(policy)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put backup policy, error: %s", string(body))
	}

	return nil
}

func (b *Client) UpdateBackupPolicy(name string, policy *model.BackupPolicy) error {
	url := b.API(fmt.Sprintf("/config/policies/%s", name))

	jsonBody, err := json.Marshal(policy)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update backup policy, error: %s", string(body))
	}

	return nil
}

func (b *Client) GetBackupRoutines() {}

func (b *Client) PutBackupRoutine(name string, routine *model.BackupRoutine) error {
	url := b.API(fmt.Sprintf("/config/routines/%s", name))

	jsonBody, err := json.Marshal(routine)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put backup routine, error: %s", string(body))
	}

	return nil
}

func (b *Client) UpdateBackupRoutine(name string, routine *model.BackupRoutine) error {
	url := b.API(fmt.Sprintf("/config/routines/%s", name))

	jsonBody, err := json.Marshal(routine)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update backup routine, error: %s", string(body))
	}

	return nil
}

func (b *Client) GetStorage() (map[string]*model.Storage, error) {
	url := b.API("/config/storage")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup storage")
	}

	storage := make(map[string]*model.Storage)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &storage); err != nil {
		return nil, err
	}

	return storage, nil
}

func (b *Client) PutStorage(name string, storage *model.Storage) error {
	url := b.API(fmt.Sprintf("/config/storage/%s", name))

	jsonBody, err := json.Marshal(storage)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put backup storage, error: %s", string(body))
	}

	return nil
}

func (b *Client) UpdateStorage(name string, storage *model.Storage) error {
	url := b.API(fmt.Sprintf("/config/storage/%s", name))

	jsonBody, err := json.Marshal(storage)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update backup storage, error: %s", string(body))
	}

	return nil
}

func (b *Client) GetFullBackups() (map[string][]model.BackupDetails, error) {
	url := b.API("/backups/full")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backups")
	}

	backups := make(map[string][]model.BackupDetails)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &backups); err != nil {
		return nil, err
	}

	return backups, nil
}

func (b *Client) GetFullBackupForRoutine(routineName string) ([]model.BackupDetails, error) {
	url := b.API(fmt.Sprintf("/backups/full/%s", routineName))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backups")
	}

	var backups []model.BackupDetails

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &backups); err != nil {
		return nil, err
	}

	return backups, nil
}

func (b *Client) ScheduleBackup(routineName string) error {
	url := b.API(fmt.Sprintf("/backups/schedule/%s", routineName))

	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("failed to schedule backup")
	}

	return nil
}

func (b *Client) TriggerFullRestore(log logr.Logger, request *model.RestoreRequest) (*int64, error) {
	url := b.API("/restore/full")

	log.Info("Triggering full restore")

	jsonBody, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusAccepted {
		log.Info("Response", "status-code", resp.StatusCode)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		return nil, fmt.Errorf("failed to trigger full restore, error: %s", string(body))
	}

	var jobID int64

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &jobID); err != nil {
		return nil, err
	}

	return &jobID, nil
}

func (b *Client) TriggerIncrementalRestore(log logr.Logger, request *model.RestoreRequest) (*int64, error) {
	url := b.API("/restore/incremental")

	jsonBody, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("failed to trigger incremental restore")
	}

	var jobID int64

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &jobID); err != nil {
		return nil, err
	}

	return &jobID, nil
}

func (b *Client) TriggerRestoreByTimeStamp(log logr.Logger, request *model.RestoreTimestampRequest) (*int64, error) {
	url := b.API("/restore/timestamp")

	jsonBody, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("failed to trigger restore by timestamp")
	}

	var jobID int64

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &jobID); err != nil {
		return nil, err
	}

	return &jobID, nil
}

func (b *Client) CheckRestoreStatus(jobID *int64) (*model.RestoreJobStatus, error) {
	url := b.API(fmt.Sprintf("/restore/status/%d", *jobID))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to check restore restoreStatus")
	}

	var restoreStatus model.RestoreJobStatus

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &restoreStatus); err != nil {
		return nil, err
	}

	return &restoreStatus, nil
}

func (b *Client) API(pattern string) string {
	contextPath := b.GetContextPath()

	if !strings.HasSuffix(contextPath, "/") {
		contextPath += "/"
	}

	address := fmt.Sprintf("%s:%d", *b.GetAddress(), *b.Port)

	return fmt.Sprintf("http://%s%s%s%s", address, contextPath, restAPIVersion, pattern)
}
