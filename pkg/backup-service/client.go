//nolint:gosec // to ignore potential HTTP request made with variable url (gosec)
package backupservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	url2 "net/url"
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	lib "github.com/aerospike/aerospike-management-lib"
)

const restAPIVersion = "v1"
const defaultContextPath = "/"
const contentTypeJSON = "application/json"

type Client struct {
	// The address to listen on.
	Address string `json:"address,omitempty"`

	// ContextPath customizes path for the API endpoints.
	ContextPath string `json:"context-path,omitempty"`

	// Version is the ABS version.
	// Used for API version-aware dispatch.
	Version string `json:"version,omitempty"`

	// The port to listen on.
	Port int32 `json:"port,omitempty"`
}

func GetBackupServiceClient(k8sClient client.Client, svc *v1beta1.BackupService) (*Client, error) {
	backupSvc := &v1beta1.AerospikeBackupService{}

	if err := k8sClient.Get(context.TODO(),
		types.NamespacedName{
			Namespace: svc.Namespace,
			Name:      svc.Name,
		}, backupSvc,
	); err != nil {
		return nil, err
	}

	version, err := asdbv1.GetImageVersion(backupSvc.Spec.Image)
	if err != nil {
		// Non-fatal: proceed without version; dispatch will use legacy API.
		version = ""
	}

	return NewClientWithVersion(
		fmt.Sprintf("%s.%s.svc", backupSvc.Name, backupSvc.Namespace),
		backupSvc.Status.Port,
		backupSvc.Status.ContextPath,
		version,
	), nil
}

func NewClient(address string, port int32, contextPath string) *Client {
	return NewClientWithVersion(address, port, contextPath, "")
}

func NewClientWithVersion(address string, port int32, contextPath, version string) *Client {
	return &Client{
		Address:     address,
		Port:        port,
		ContextPath: contextPath,
		Version:     version,
	}
}

func (c *Client) getAddress() string {
	return c.Address
}

func (c *Client) getPort() int32 {
	return c.Port
}

func (c *Client) getContextPath() string {
	if c.ContextPath != "" {
		return c.ContextPath
	}

	return defaultContextPath
}

func (c *Client) CheckBackupServiceHealth() error {
	url := c.API("/health")

	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("backup service is not healthy")
	}

	return nil
}

func (c *Client) GetBackupServiceConfig() (map[string]interface{}, error) {
	url := c.API("/config")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup service config")
	}

	conf := make(map[string]interface{})

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &conf); err != nil {
		return nil, err
	}

	return conf, nil
}

func (c *Client) ApplyConfig() error {
	url := c.API("/config/apply")

	resp, err := http.Post(url, contentTypeJSON, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to apply latest config, error: %s", string(body))
	}

	return nil
}

func (c *Client) GetClusters() (map[string]interface{}, error) {
	url := c.API("/config/clusters")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get aerospike clusters")
	}

	aerospikeClusters := make(map[string]interface{})

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &aerospikeClusters); err != nil {
		return nil, err
	}

	return aerospikeClusters, nil
}

func (c *Client) PutCluster(name, cluster interface{}) error {
	url := c.API(fmt.Sprintf("/config/clusters/%s", name))

	jsonBody, err := json.Marshal(cluster)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	cl := &http.Client{}

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put aerospike cluster, error: %s", string(body))
	}

	return nil
}

func (c *Client) DeleteCluster(name string) error {
	url := c.API(fmt.Sprintf("/config/clusters/%s", name))

	req, err := http.NewRequest(http.MethodDelete, url, http.NoBody)
	if err != nil {
		return err
	}

	cl := &http.Client{}

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to delete aerospike cluster, error: %s", string(body))
	}

	return nil
}

func (c *Client) AddCluster(name, cluster interface{}) error {
	url := c.API(fmt.Sprintf("/config/clusters/%s", name))

	jsonBody, err := json.Marshal(cluster)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, contentTypeJSON, bodyReader)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update aerospike cluster, error: %s", string(body))
	}

	return nil
}

func (c *Client) GetBackupPolicies() (map[string]interface{}, error) {
	url := c.API("/config/policies")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup policies")
	}

	policies := make(map[string]interface{})

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &policies); err != nil {
		return nil, err
	}

	return policies, nil
}

func (c *Client) PutBackupPolicy(name string, policy interface{}) error {
	url := c.API(fmt.Sprintf("/config/policies/%s", name))

	jsonBody, err := json.Marshal(policy)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	cl := &http.Client{}

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put backup policy, error: %s", string(body))
	}

	return nil
}

func (c *Client) AddBackupPolicy(name string, policy interface{}) error {
	url := c.API(fmt.Sprintf("/config/policies/%s", name))

	jsonBody, err := json.Marshal(policy)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, contentTypeJSON, bodyReader)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update backup policy, error: %s", string(body))
	}

	return nil
}

func (c *Client) GetBackupRoutines() {}

func (c *Client) PutBackupRoutine(name string, routine interface{}) error {
	url := c.API(fmt.Sprintf("/config/routines/%s", name))

	jsonBody, err := json.Marshal(routine)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	cl := &http.Client{}

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put backup routine, error: %s", string(body))
	}

	return nil
}

func (c *Client) AddBackupRoutine(name string, routine interface{}) error {
	url := c.API(fmt.Sprintf("/config/routines/%s", name))

	jsonBody, err := json.Marshal(routine)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, contentTypeJSON, bodyReader)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update backup routine, error: %s", string(body))
	}

	return nil
}

func (c *Client) DeleteBackupRoutine(name string) error {
	url := c.API(fmt.Sprintf("/config/routines/%s", name))

	req, err := http.NewRequest(http.MethodDelete, url, http.NoBody)
	if err != nil {
		return err
	}

	cl := &http.Client{}

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to delete backup routine, error: %s", string(body))
	}

	return nil
}

func (c *Client) GetStorage() (map[string]interface{}, error) {
	url := c.API("/config/storage")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup storage")
	}

	storage := make(map[string]interface{})

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &storage); err != nil {
		return nil, err
	}

	return storage, nil
}

func (c *Client) PutStorage(name string, storage interface{}) error {
	url := c.API(fmt.Sprintf("/config/storage/%s", name))

	jsonBody, err := json.Marshal(storage)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return err
	}

	cl := &http.Client{}

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to put backup storage, error: %s", string(body))
	}

	return nil
}

func (c *Client) AddStorage(name string, storage interface{}) error {
	url := c.API(fmt.Sprintf("/config/storage/%s", name))

	jsonBody, err := json.Marshal(storage)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, contentTypeJSON, bodyReader)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to update backup storage, error: %s", string(body))
	}

	return nil
}

func (c *Client) GetFullBackups() (map[string][]interface{}, error) {
	url := c.API("/backups/full")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backups")
	}

	backups := make(map[string][]interface{})

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &backups); err != nil {
		return nil, err
	}

	return backups, nil
}

func (c *Client) GetFullBackupsForRoutine(routineName string) ([]interface{}, error) {
	url := c.API(fmt.Sprintf("/backups/full/%s", routineName))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get full backups")
	}

	var backups []interface{}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &backups); err != nil {
		return nil, err
	}

	return backups, nil
}

func (c *Client) GetIncrementalBackupsForRoutine(routineName string) ([]interface{}, error) {
	url := c.API(fmt.Sprintf("/backups/incremental/%s", routineName))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get incremental backups")
	}

	var backups []interface{}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &backups); err != nil {
		return nil, err
	}

	return backups, nil
}

// Deprecated: ScheduleBackup API is deprecated in ABS >= 3.5.0
// For ABS >= 3.5.0: uses TriggerOnDemandBackup func
func (c *Client) ScheduleBackup(routineName string, delay metav1.Duration) error {
	url, err := url2.Parse(c.API(fmt.Sprintf("/backups/schedule/%s", routineName)))
	if err != nil {
		return err
	}

	if delay.Milliseconds() > 0 {
		query := url.Query()
		query.Add("delay", fmt.Sprintf("%d", delay.Milliseconds()))
		url.RawQuery = query.Encode()
	}

	resp, err := http.Post(url.String(), contentTypeJSON, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("failed to schedule backup")
	}

	return nil
}

// TriggerFullBackup triggers a full on-demand backup for the given routine.
// Requires ABS >= 3.5.0.
func (c *Client) TriggerFullBackup(routineName string, delay metav1.Duration) error {
	return c.triggerOnDemandBackup("full", routineName, delay)
}

// TriggerIncrementalBackup triggers an incremental on-demand backup for the given routine.
// Requires ABS >= 3.5.0.
func (c *Client) TriggerIncrementalBackup(routineName string, delay metav1.Duration) error {
	return c.triggerOnDemandBackup("incremental", routineName, delay)
}

func (c *Client) triggerOnDemandBackup(backupType, routineName string, delay metav1.Duration) error {
	parsedURL, err := url2.Parse(c.API(fmt.Sprintf("/backups/%s/%s", backupType, routineName)))
	if err != nil {
		return err
	}

	if delay.Milliseconds() > 0 {
		query := parsedURL.Query()
		query.Add("delay", fmt.Sprintf("%d", delay.Milliseconds()))
		parsedURL.RawQuery = query.Encode()
	}

	resp, err := http.Post(parsedURL.String(), contentTypeJSON, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("failed to trigger %s backup", backupType)
		}

		return fmt.Errorf("failed to trigger %s backup, error: %s", backupType, string(body))
	}

	return nil
}

// TriggerOnDemandBackup dispatches an on-demand backup request using the appropriate API
// based on the ABS version stored in the client.
// For ABS >= 3.5.0: uses POST /v1/backups/full/{name} or POST /v1/backups/incremental/{name}.
// For ABS < 3.5.0: falls back to the deprecated POST /v1/backups/schedule/{name} (full only).
func (c *Client) TriggerOnDemandBackup(routineName string, backupType v1beta1.BackupType, delay metav1.Duration) error {
	useNewAPI, err := c.supportsNewOnDemandAPI()
	if err != nil {
		return fmt.Errorf("failed to determine ABS version for on-demand backup dispatch: %w", err)
	}

	if useNewAPI {
		switch backupType {
		case v1beta1.IncrementalBackup:
			return c.TriggerIncrementalBackup(routineName, delay)
		case v1beta1.FullBackup:
			return c.TriggerFullBackup(routineName, delay)
		default:
			return fmt.Errorf("unknown backup type %s", backupType)
		}
	}

	// Legacy path: ABS < 3.5.0 only supports full backups via /backups/schedule/{name}.
	if backupType == v1beta1.IncrementalBackup {
		return fmt.Errorf(
			"%s on-demand backups require ABS >= %s; current version: %s",
			v1beta1.IncrementalBackup, v1beta1.BackupSvcNewOnDemandAPIVersion, c.Version,
		)
	}

	return c.ScheduleBackup(routineName, delay)
}

// supportsNewOnDemandAPI returns true when the ABS version is >= 3.5.0.
// If the version is empty or cannot be parsed, it returns false (safe fallback to legacy API).
func (c *Client) supportsNewOnDemandAPI() (bool, error) {
	if c.Version == "" {
		return false, nil
	}

	cmp, err := lib.CompareVersions(c.Version, v1beta1.BackupSvcNewOnDemandAPIVersion)
	if err != nil {
		return false, err
	}

	return cmp >= 0, nil
}

func (c *Client) TriggerRestoreWithType(log logr.Logger, restoreType string,
	request []byte) (jobID *int64, statusCode *int, err error) {
	log.Info(fmt.Sprintf("Triggering %s restore", restoreType))

	var url string

	switch restoreType {
	case "Full":
		url = c.API("/restore/full")

	case "Incremental":
		url = c.API("/restore/incremental")

	case "Timestamp":
		url = c.API("/restore/timestamp")

	default:
		return nil, nil, fmt.Errorf("unsupported restore type")
	}

	jsonBody, err := yaml.YAMLToJSON(request)
	if err != nil {
		return nil, nil, err
	}

	bodyReader := bytes.NewReader(jsonBody)

	resp, err := http.Post(url, contentTypeJSON, bodyReader)
	if err != nil {
		return nil, nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		log.Info("Response", "status-code", resp.StatusCode)

		body, rErr := io.ReadAll(resp.Body)
		if rErr != nil {
			return nil, &resp.StatusCode, rErr
		}

		return nil, &resp.StatusCode,
			fmt.Errorf("failed to trigger %s restore, error: %s", restoreType, string(body))
	}

	jobID = new(int64)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &resp.StatusCode, err
	}

	if err := json.Unmarshal(body, jobID); err != nil {
		return nil, &resp.StatusCode, err
	}

	log.Info(fmt.Sprintf("Triggered %s restore", restoreType))

	return jobID, &resp.StatusCode, nil
}

func (c *Client) CheckRestoreStatus(jobID int64) (map[string]interface{}, error) {
	url := c.API(fmt.Sprintf("/restore/status/%d", jobID))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to check restore restoreStatus")
	}

	restoreStatus := make(map[string]interface{})

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &restoreStatus); err != nil {
		return nil, err
	}

	return restoreStatus, nil
}

func (c *Client) CancelRestoreJob(jobID int64) (int, error) {
	url := c.API(fmt.Sprintf("/restore/cancel/%d", jobID))

	resp, err := http.Post(url, contentTypeJSON, nil)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	// Check if the response status code is 200 OK or 202 Accepted
	// 200 OK is for ABS < 3.1.0 and 202 Accepted is for ABS >= 3.1.0
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, err
		}

		return resp.StatusCode, fmt.Errorf("failed to cancel restore job, error: %s", string(body))
	}

	return resp.StatusCode, nil
}

func (c *Client) API(pattern string) string {
	contextPath := c.getContextPath()

	if !strings.HasSuffix(contextPath, "/") {
		contextPath += "/"
	}

	address := fmt.Sprintf("%s:%d", c.getAddress(), c.getPort())

	return fmt.Sprintf("http://%s%s%s%s", address, contextPath, restAPIVersion, pattern)
}
