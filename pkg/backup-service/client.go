package backupservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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

	var conf *model.Config

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, conf); err != nil {
		return nil, err
	}

	return conf, nil
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

	var backups map[string][]model.BackupDetails

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

func (b *Client) GetBackupPolicies() (map[string]*model.BackupPolicy, error) {
	url := b.API("/config/policies")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup policies")
	}

	var policies map[string]*model.BackupPolicy

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

func (b *Client) GetBackupPolicy() {}

func (b *Client) GetBackupRoutines() {}

func (b *Client) GetBackupRoutine() {}

func (b *Client) TriggerFullRestore(request *model.RestoreRequest) (int64, error) {
	url := b.API("/restore/full")

	jsonBody, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("failed to trigger full restore")
	}

	var jobID int64

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if err := json.Unmarshal(body, &jobID); err != nil {
		return 0, err
	}

	return jobID, nil
}

func (b *Client) TriggerIncrementalRestore(request *model.RestoreRequest) (int64, error) {
	url := b.API("/restore/incremental")

	jsonBody, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("failed to trigger incremental restore")
	}

	var jobID int64

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if err := json.Unmarshal(body, &jobID); err != nil {
		return 0, err
	}

	return jobID, nil
}

func (b *Client) TriggerRestoreByTimeStamp(request *model.RestoreTimestampRequest) (int64, error) {
	url := b.API("/restore/timestamp")

	jsonBody, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, "application/json", bodyReader)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("failed to trigger restore by timestamp")
	}

	var jobID int64

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if err := json.Unmarshal(body, &jobID); err != nil {
		return 0, err
	}

	return jobID, nil
}

func (b *Client) CheckRestoreStatus(jobID int64) (*model.RestoreJobStatus, error) {
	url := b.API(fmt.Sprintf("/restore/restoreStatus/%d", jobID))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to check restore restoreStatus")
	}

	var restoreStatus *model.RestoreJobStatus

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, restoreStatus); err != nil {
		return nil, err
	}

	return restoreStatus, nil
}

func (b *Client) API(pattern string) string {
	contextPath := b.GetContextPath()

	if !strings.HasSuffix(contextPath, "/") {
		contextPath += "/"
	}

	return fmt.Sprintf("%s%s%s", contextPath, restAPIVersion, pattern)
}
