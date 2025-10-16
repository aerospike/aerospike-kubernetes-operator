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
	lib "github.com/aerospike/aerospike-management-lib"
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

//nolint:lll // for readability
// +kubebuilder:webhook:path=/validate-eviction,mutating=false,failurePolicy=ignore,sideEffects=None,groups="",resources=pods/eviction, verbs=create,versions=v1,name=veviction.kb.io,admissionReviewVersions={v1}

// Handle handles eviction requests and blocks eviction of Aerospike pods
func (ew *EvictionWebhook) Handle(w http.ResponseWriter, r *http.Request) {
	var enableSafePodEviction = "ENABLE_SAFE_POD_EVICTION"

	log := ew.Log.WithValues("method", r.Method, "url", r.URL.Path)
	log.Info("Received eviction webhook request")

	// Parse the admission review
	var admissionReview admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&admissionReview); err != nil {
		log.Error(err, "Failed to decode admission review")
		http.Error(w, "Failed to decode admission review", http.StatusBadRequest)

		return
	}

	// Create response
	response := admissionv1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: true,
	}

	enable, found := os.LookupEnv(enableSafePodEviction)
	if !found || !strings.EqualFold(enable, "true") {
		log.V(1).Info("Safe pod eviction is disabled via environment variable", "env", enableSafePodEviction)
		return
	}

	shouldEvaluate := true

	watchNs, err := asdbv1.GetWatchNamespace()
	if err != nil {
		response.Allowed = false
		response.Result = &metav1.Status{
			Status:  metav1.StatusFailure,
			Message: fmt.Sprintf("Failed to get watch namespaces: %v", err),
			Code:    http.StatusInternalServerError,
		}

		shouldEvaluate = false
	} else if watchNs != "" {
		nsList := strings.Split(watchNs, ",")
		if !lib.ContainsString(nsList, admissionReview.Request.Namespace) {
			shouldEvaluate = false
		}
	}

	if shouldEvaluate {
		// Decode the eviction object
		var eviction policyv1.Eviction
		if err := json.Unmarshal(admissionReview.Request.Object.Raw, &eviction); err != nil {
			log.Error(err, "Failed to unmarshal eviction object")

			response.Allowed = false
			response.Result = &metav1.Status{
				Status:  metav1.StatusFailure,
				Message: fmt.Sprintf("Failed to unmarshal eviction object: %v", err),
				Code:    http.StatusBadRequest,
			}
		} else {
			// Get the pod that is being evicted
			pod := &corev1.Pod{}
			podKey := types.NamespacedName{
				Name:      eviction.Name,
				Namespace: admissionReview.Request.Namespace,
			}

			if err := ew.Client.Get(context.Background(), podKey, pod); err != nil {
				log.Error(err, "Failed to get pod for eviction", "pod", podKey)

				response.Allowed = false
				response.Result = &metav1.Status{
					Status:  metav1.StatusFailure,
					Message: fmt.Sprintf("Failed to get pod %s/%s: %v", admissionReview.Request.Namespace, eviction.Name, err),
					Code:    http.StatusInternalServerError,
				}
			} else {
				// Check if this is an Aerospike pod
				if ew.isAerospikePod(pod) {
					log.Info("Blocking eviction of Aerospike pod", "pod", podKey)

					// Set annotation on the pod to indicate eviction was blocked
					if err := ew.setEvictionBlockedAnnotation(context.Background(), pod); err != nil {
						log.Error(err, "Failed to set eviction blocked annotation", "pod", podKey)
						// Continue with blocking eviction even if annotation fails
					}

					response.Allowed = false
					response.Result = &metav1.Status{
						Status: metav1.StatusFailure,
						Message: fmt.Sprintf("Eviction of Aerospike pod %s/%s is blocked by admission webhook",
							admissionReview.Request.Namespace, eviction.Name),
						Reason: EvictionBlockedReason,
						Code:   http.StatusForbidden,
					}
				} else {
					log.V(1).Info("Allowing eviction of non-Aerospike pod", "pod", podKey)
				}
			}
		}
	}

	// Create response admission review
	admissionReview.Response = &response
	admissionReview.Request = nil // Clear request to reduce response size

	// Send response
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
		log.Error(err, "Failed to encode admission review response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}

	log.Info("Eviction webhook request processed", "allowed", response.Allowed)
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
