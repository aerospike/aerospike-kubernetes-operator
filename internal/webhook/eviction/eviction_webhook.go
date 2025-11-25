/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package general

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

const (
	// EvictionBlockedAnnotation is the annotation set on pods when eviction is blocked
	EvictionBlockedAnnotation = "aerospike.com/eviction-blocked"
	EvictionAllowed           = "allowed"
	EvictionBlocked           = "blocked"
)

var (
	// evictionRequestsTotal tracks the total number of eviction requests processed
	evictionRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aerospike_ako_eviction_webhook_requests_total",
			Help: "Total number of pod eviction requests processed by the webhook",
		},
		[]string{"eviction_namespace", "decision"},
	)
)

// +kubebuilder:object:generate=false
// Above marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type EvictionWebhook struct {
	Client    client.Client // Cache-backed client (fast for Aerospike pods)
	APIReader client.Reader // Direct API server reader (fallback for non-Aerospike pods)
	Log       logr.Logger
	Enable    bool
}

// isAerospikePod checks if the given pod is an Aerospike pod
func (ew *EvictionWebhook) isAerospikePod(pod *corev1.Pod) bool {
	labels := pod.GetLabels()
	if labels == nil {
		return false
	}

	// Check for Aerospike-specific labels
	appLabel, hasAppLabel := labels[asdbv1.AerospikeAppLabel]
	_, hasCustomResourceLabel := labels[asdbv1.AerospikeCustomResourceLabel]

	// Pod is considered an Aerospike pod if it has both required labels
	return hasAppLabel && appLabel == asdbv1.AerospikeAppLabelValue && hasCustomResourceLabel
}

// setEvictionBlockedAnnotation sets an annotation on the pod indicating eviction was blocked
func (ew *EvictionWebhook) setEvictionBlockedAnnotation(ctx context.Context, pod *corev1.Pod) error {
	// Check if annotation already exists, no update needed
	if pod.Annotations != nil {
		if _, exists := pod.Annotations[EvictionBlockedAnnotation]; exists {
			return nil
		}
	}

	// Create a patch to add the annotation
	patch := client.MergeFrom(pod.DeepCopy())

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	pod.Annotations[EvictionBlockedAnnotation] = time.Now().Format(time.RFC3339)

	return ew.Client.Patch(ctx, pod, patch)
}

// SetupEvictionWebhookWithManager registers the eviction webhook with the manager
func SetupEvictionWebhookWithManager(mgr ctrl.Manager) *EvictionWebhook {
	ew := &EvictionWebhook{
		Client:    mgr.GetClient(),    // Cache-backed client (fast for Aerospike pods)
		APIReader: mgr.GetAPIReader(), // Direct API server reader (fallback for non-Aerospike pods)
		Log:       logf.Log.WithName("eviction-webhook"),
	}

	enable, found := os.LookupEnv("ENABLE_SAFE_POD_EVICTION")

	ew.Enable = found && strings.EqualFold(enable, "true")

	// Register metrics only if webhook is enabled
	if ew.Enable {
		metrics.Registry.MustRegister(evictionRequestsTotal)
		ew.Log.Info("Eviction webhook metrics registered")
	}

	// Register the webhook using the webhook server with direct HTTP handler
	webhookServer := mgr.GetWebhookServer()
	webhookServer.Register("/validate-eviction", http.HandlerFunc(ew.Handle))

	return ew
}

//nolint:lll // for readability
// +kubebuilder:webhook:path=/validate-eviction,mutating=false,failurePolicy=ignore,sideEffects=None,groups="",resources=pods/eviction,verbs=create,versions=v1,timeoutSeconds=20,name=vaerospikeeviction.kb.io,admissionReviewVersions={v1}

func (ew *EvictionWebhook) Handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := ew.Log.WithValues("method", r.Method, "url", r.URL.Path)
	log.Info("Received pod eviction webhook request")

	// Parse admission review
	admissionReview, err := ew.parseAdmissionReview(r)
	if err != nil {
		log.Error(err, "Failed to parse admission review")
		ew.sendErrorResponse(w, http.StatusBadRequest, "Failed to parse admission review")

		return
	}

	// Check if webhook is enabled
	if !ew.isWebhookEnabled() {
		log.V(1).Info("Safe pod eviction is disabled via environment variable, skipping request processing")
		ew.sendResponse(w, admissionReview, getSuccessResponse(admissionReview.Request.UID))

		return
	}

	// Process eviction request
	response := ew.processEvictionRequest(ctx, admissionReview, log)

	// Send final response
	ew.sendResponse(w, admissionReview, response)
	log.Info("Eviction webhook request processed", "allowed", response.Allowed)
}

// parseAdmissionReview parses the admission review from the request
func (ew *EvictionWebhook) parseAdmissionReview(r *http.Request) (*admissionv1.AdmissionReview, error) {
	var admissionReview admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&admissionReview); err != nil {
		return nil, fmt.Errorf("failed to decode admission review: %w", err)
	}

	if admissionReview.Request == nil {
		return nil, fmt.Errorf("admission review request is nil")
	}

	return &admissionReview, nil
}

// isWebhookEnabled checks if the eviction webhook is enabled
func (ew *EvictionWebhook) isWebhookEnabled() bool {
	return ew.Enable
}

// recordMetric records an eviction webhook metric if the webhook is enabled
func (ew *EvictionWebhook) recordMetric(namespace, decision string) {
	if ew.Enable {
		evictionRequestsTotal.WithLabelValues(namespace, decision).Inc()
	}
}

// processEvictionRequest processes the eviction request and returns the response
func (ew *EvictionWebhook) processEvictionRequest(ctx context.Context, admissionReview *admissionv1.AdmissionReview,
	log logr.Logger) *admissionv1.AdmissionResponse {
	// Parse eviction object
	eviction, err := ew.parseEvictionObject(admissionReview.Request.Object.Raw)
	if err != nil {
		log.Error(err, "Failed to parse eviction object")

		return getFailureResponse(
			admissionReview.Request.UID,
			fmt.Sprintf("Failed to parse eviction object: %v", err),
			metav1.StatusReasonBadRequest,
			http.StatusBadRequest)
	}

	// Get pod information
	pod, err := ew.getPodForEviction(ctx, eviction, admissionReview.Request.Namespace)
	if err != nil {
		// If pod doesn't exist, allow the eviction request to proceed
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Pod not found, allowing eviction request to proceed", "pod", eviction.Name)
			ew.recordMetric(admissionReview.Request.Namespace, EvictionAllowed)

			return getSuccessResponse(admissionReview.Request.UID)
		}

		// For other errors (timeout, permission, etc.), return error
		log.Error(err, "Failed to get pod for eviction", "pod", eviction.Name)

		return getFailureResponse(
			admissionReview.Request.UID,
			err.Error(),
			metav1.StatusReasonInternalError,
			http.StatusInternalServerError,
		)
	}

	// Check if this is an Aerospike pod (runtime filtering required for pods/eviction subresource)
	if !ew.isAerospikePod(pod) {
		log.V(1).Info("Allowing eviction of non-Aerospike pod", "pod", eviction.Name)
		ew.recordMetric(admissionReview.Request.Namespace, EvictionAllowed)

		return getSuccessResponse(admissionReview.Request.UID)
	}

	// Block Aerospike pod eviction
	log.Info("Blocking eviction of Aerospike pod", "pod", eviction.Name)

	if err := ew.setEvictionBlockedAnnotation(ctx, pod); err != nil {
		log.Info("Failed to set eviction blocked annotation (eviction still blocked)",
			"pod", pod.Name, "namespace", pod.Namespace, "error", err)

		return getFailureResponse(
			admissionReview.Request.UID,
			fmt.Sprintf("Failed to set eviction blocked annotation on pod %s/%s: %v",
				admissionReview.Request.Namespace, eviction.Name, err),
			metav1.StatusReasonInternalError,
			http.StatusInternalServerError,
		)
	}

	ew.recordMetric(admissionReview.Request.Namespace, EvictionBlocked)
	// Block eviction regardless of annotation success/failure
	return getFailureResponse(
		admissionReview.Request.UID,
		fmt.Sprintf("Eviction of Aerospike pod %s/%s is blocked by admission webhook,"+
			" Aerospike Operator will handle Aerospike pod eviction.",
			admissionReview.Request.Namespace, eviction.Name),
		metav1.StatusReasonForbidden,
		http.StatusForbidden,
	)
}

// parseEvictionObject parses the eviction object from raw bytes
func (ew *EvictionWebhook) parseEvictionObject(raw []byte) (*policyv1.Eviction, error) {
	var eviction policyv1.Eviction
	if err := json.Unmarshal(raw, &eviction); err != nil {
		return nil, fmt.Errorf("failed to unmarshal eviction object: %w", err)
	}

	return &eviction, nil
}

// getPodForEviction retrieves the pod that is being evicted
// It tries the cache first (fast path for Aerospike pods), and falls back to
// the API server if not found (for non-Aerospike pods filtered out by cache)
func (ew *EvictionWebhook) getPodForEviction(
	ctx context.Context, eviction *policyv1.Eviction, namespace string,
) (*corev1.Pod, error) {
	podKey := types.NamespacedName{
		Name:      eviction.Name,
		Namespace: namespace,
	}

	pod := &corev1.Pod{}

	// Try cache first (fast path for Aerospike pods)
	err := ew.Client.Get(ctx, podKey, pod)
	if err == nil {
		return pod, nil
	}

	// If not found in cache, try API server directly
	// This handles:
	// 1. Non-Aerospike pods (filtered out by cache label selector)
	// 2. Aerospike pods not yet in cache (race condition, cache sync delay, etc.)
	if apierrors.IsNotFound(err) {
		ew.Log.V(1).Info("Pod not in cache, checking API server", "pod", podKey.String())

		if err = ew.APIReader.Get(ctx, podKey, pod); err != nil {
			return nil, fmt.Errorf("failed to get pod %s: %w", podKey.String(), err)
		}

		return pod, nil
	}

	// Other errors (not NotFound) are returned as-is
	return nil, fmt.Errorf("failed to get pod %s from cache: %w", podKey.String(), err)
}

// sendResponse sends the admission review response
func (ew *EvictionWebhook) sendResponse(w http.ResponseWriter, admissionReview *admissionv1.AdmissionReview,
	response *admissionv1.AdmissionResponse) {
	admissionReview.Response = response
	admissionReview.Request = nil // Clear request to reduce response size

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
		ew.Log.Error(err, "Failed to encode admission review response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// sendErrorResponse sends an error response
func (ew *EvictionWebhook) sendErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	http.Error(w, message, statusCode)
}

func getSuccessResponse(requestID types.UID) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		UID:     requestID,
		Allowed: true,
	}
}

// TODO: Finalise the error codes and reasons used here.
func getFailureResponse(requestID types.UID, message string, reason metav1.StatusReason, code int32,
) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		UID:     requestID,
		Allowed: false,
		Result: &metav1.Status{
			Status:  metav1.StatusFailure,
			Message: message,
			Reason:  reason,
			Code:    code,
		},
	}
}
