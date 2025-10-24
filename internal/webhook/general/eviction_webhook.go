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
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

const (
	// EvictionBlockedAnnotation is the annotation set on pods when eviction is blocked
	EvictionBlockedAnnotation = "aerospike.com/eviction-blocked"
	// EvictionBlockedReason is the reason for blocking eviction
	EvictionBlockedReason = "AerospikePodEvictionBlocked"
)

// +kubebuilder:object:generate=false
// Above marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type EvictionWebhook struct {
	Client client.Client
	Log    logr.Logger
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
func SetupEvictionWebhookWithManager(mgr ctrl.Manager) error {
	ew := &EvictionWebhook{
		Client: mgr.GetClient(),
		Log:    logf.Log.WithName("eviction-webhook"),
	}

	// Register the webhook using the webhook server with direct HTTP handler
	webhookServer := mgr.GetWebhookServer()
	webhookServer.Register("/validate-eviction", http.HandlerFunc(ew.Handle))

	return nil
}

//nolint:lll // for readability
// +kubebuilder:webhook:path=/validate-eviction,mutating=false,failurePolicy=ignore,sideEffects=None,groups="",resources=pods/eviction, verbs=create,versions=v1,name=veviction.kb.io,admissionReviewVersions={v1}

func (ew *EvictionWebhook) Handle(w http.ResponseWriter, r *http.Request) {
	log := ew.Log.WithValues("method", r.Method, "url", r.URL.Path)
	log.Info("Received eviction webhook request")

	// Parse admission review
	admissionReview, err := ew.parseAdmissionReview(r)
	if err != nil {
		log.Error(err, "Failed to parse admission review")
		ew.sendErrorResponse(w, http.StatusBadRequest, "Failed to parse admission review")

		return
	}

	// Create base response
	response := admissionv1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: true,
	}

	// Check if webhook is enabled
	if !ew.isWebhookEnabled() {
		log.V(1).Info("Safe pod eviction is disabled via environment variable")
		ew.sendResponse(w, admissionReview, &response)

		return
	}

	// Process eviction request
	evictionResult := ew.processEvictionRequest(admissionReview, log)
	if evictionResult != nil {
		response = *evictionResult
	}

	// Send final response
	ew.sendResponse(w, admissionReview, &response)
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

// isWebhookEnabled checks if the webhook is enabled via environment variable
func (ew *EvictionWebhook) isWebhookEnabled() bool {
	enable, found := os.LookupEnv("ENABLE_SAFE_POD_EVICTION")
	return found && strings.EqualFold(enable, "true")
}

// processEvictionRequest processes the eviction request and returns the response
func (ew *EvictionWebhook) processEvictionRequest(admissionReview *admissionv1.AdmissionReview,
	log logr.Logger) *admissionv1.AdmissionResponse {
	// Parse eviction object
	eviction, err := ew.parseEvictionObject(admissionReview.Request.Object.Raw)
	if err != nil {
		log.Error(err, "Failed to parse eviction object")

		return &admissionv1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Status:  metav1.StatusFailure,
				Message: fmt.Sprintf("Failed to parse eviction object: %v", err),
				Code:    http.StatusBadRequest,
			},
		}
	}

	// Get pod information
	pod, err := ew.getPodForEviction(eviction, admissionReview.Request.Namespace)
	if err != nil {
		log.Error(err, "Failed to get pod for eviction", "pod", eviction.Name)

		return &admissionv1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Status:  metav1.StatusFailure,
				Message: fmt.Sprintf("Failed to get pod %s/%s: %v", admissionReview.Request.Namespace, eviction.Name, err),
				Code:    http.StatusInternalServerError,
			},
		}
	}

	// Check if this is an Aerospike pod
	if !ew.isAerospikePod(pod) {
		log.V(1).Info("Allowing eviction of non-Aerospike pod", "pod", eviction.Name)
		return nil // Allow eviction
	}

	// Block Aerospike pod eviction
	log.Info("Blocking eviction of Aerospike pod", "pod", eviction.Name)

	// Set annotation asynchronously (non-blocking)
	// TODO: do we really want async here?
	go ew.setEvictionBlockedAnnotationAsync(pod)

	return &admissionv1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: false,
		Result: &metav1.Status{
			Status: metav1.StatusFailure,
			Message: fmt.Sprintf("Eviction of Aerospike pod %s/%s is blocked by admission webhook",
				admissionReview.Request.Namespace, eviction.Name),
			Reason: EvictionBlockedReason,
			Code:   http.StatusForbidden,
		},
	}
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
func (ew *EvictionWebhook) getPodForEviction(eviction *policyv1.Eviction, namespace string) (*corev1.Pod, error) {
	podKey := types.NamespacedName{
		Name:      eviction.Name,
		Namespace: namespace,
	}

	pod := &corev1.Pod{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ew.Client.Get(ctx, podKey, pod); err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", podKey.String(), err)
	}

	return pod, nil
}

// setEvictionBlockedAnnotationAsync sets the eviction blocked annotation asynchronously
func (ew *EvictionWebhook) setEvictionBlockedAnnotationAsync(pod *corev1.Pod) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := ew.setEvictionBlockedAnnotation(ctx, pod); err != nil {
		ew.Log.V(1).Info("Failed to set eviction blocked annotation",
			"pod", pod.Name, "error", err)
	}
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
