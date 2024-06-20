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

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

const restAPIVersion = "v1"
const defaultContextPath = "/"

type Client struct {
	// The address to listen on.
	Address string `json:"address,omitempty"`

	// ContextPath customizes path for the API endpoints.
	ContextPath string `json:"context-path,omitempty"`

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

	return &Client{
		Address:     fmt.Sprintf("%s.%s.svc", backupSvc.Name, backupSvc.Namespace),
		Port:        backupSvc.Status.Port,
		ContextPath: backupSvc.Status.ContextPath,
	}, nil
}

func (c *Client) GetAddress() string {
	return c.Address
}

func (c *Client) GetPort() int32 {
	return c.Port
}

func (c *Client) GetContextPath() string {
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup service config")
	}

	conf := make(map[string]interface{})

	defer resp.Body.Close()

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

func (c *Client) GetClusters() (map[string]interface{}, error) {
	url := c.API("/config/clusters")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get aerospike clusters")
	}

	aerospikeClusters := make(map[string]interface{})

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

	if resp.StatusCode != http.StatusNoContent {
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("failed to delete aerospike cluster, error: %s", string(body))
	}

	return nil
}

func (c *Client) UpdateCluster(name, cluster interface{}) error {
	url := c.API(fmt.Sprintf("/config/clusters/%s", name))

	jsonBody, err := json.Marshal(cluster)
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

func (c *Client) GetBackupPolicies() (map[string]interface{}, error) {
	url := c.API("/config/policies")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup policies")
	}

	policies := make(map[string]interface{})

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

func (c *Client) UpdateBackupPolicy(name string, policy interface{}) error {
	url := c.API(fmt.Sprintf("/config/policies/%s", name))

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

func (c *Client) UpdateBackupRoutine(name string, routine interface{}) error {
	url := c.API(fmt.Sprintf("/config/routines/%s", name))

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

	if resp.StatusCode != http.StatusNoContent {
		defer resp.Body.Close()

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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backup storage")
	}

	storage := make(map[string]interface{})

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

func (c *Client) UpdateStorage(name string, storage interface{}) error {
	url := c.API(fmt.Sprintf("/config/storage/%s", name))

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

func (c *Client) GetFullBackups() (map[string][]interface{}, error) {
	url := c.API("/backups/full")

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backups")
	}

	backups := make(map[string][]interface{})

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

func (c *Client) GetFullBackupForRoutine(routineName string) ([]interface{}, error) {
	url := c.API(fmt.Sprintf("/backups/full/%s", routineName))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get backups")
	}

	var backups []interface{}

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

func (c *Client) ScheduleBackup(routineName string, delay metav1.Duration) error {
	url, err := url2.Parse(c.API(fmt.Sprintf("/backups/schedule/%s", routineName)))
	if err != nil {
		return err
	}

	if delay.Duration.Milliseconds() > 0 {
		query := url.Query()
		query.Add("delay", fmt.Sprintf("%d", delay.Duration.Milliseconds()))
		url.RawQuery = query.Encode()
	}

	resp, err := http.Post(url.String(), "application/json", nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("failed to schedule backup")
	}

	return nil
}

func (c *Client) TriggerRestoreWithType(log logr.Logger, restoreType string, request []byte) (*int64, error) {
	log.Info(fmt.Sprintf("Triggering %s restore", restoreType))

	var url string

	switch restoreType {
	case "Full":
		url = c.API("/restore/full")

	case "Incremental":
		url = c.API("/restore/incremental")

	case "TimeStamp":
		url = c.API("/restore/timestamp")

	default:
		return nil, fmt.Errorf("unsupported restore type")
	}

	jsonBody, err := yaml.YAMLToJSON(request)
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

		body, rErr := io.ReadAll(resp.Body)
		if rErr != nil {
			return nil, rErr
		}

		return nil, fmt.Errorf("failed to trigger %s restore, error: %s", restoreType, string(body))
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

	log.Info(fmt.Sprintf("Triggered %s restore", restoreType))

	return &jobID, nil
}

func (c *Client) CheckRestoreStatus(jobID *int64) (map[string]interface{}, error) {
	url := c.API(fmt.Sprintf("/restore/status/%d", *jobID))

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to check restore restoreStatus")
	}

	restoreStatus := make(map[string]interface{})

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &restoreStatus); err != nil {
		return nil, err
	}

	return restoreStatus, nil
}

func (c *Client) API(pattern string) string {
	contextPath := c.GetContextPath()

	if !strings.HasSuffix(contextPath, "/") {
		contextPath += "/"
	}

	address := fmt.Sprintf("%s:%d", c.GetAddress(), c.Port)

	return fmt.Sprintf("http://%s%s%s%s", address, contextPath, restAPIVersion, pattern)
}
