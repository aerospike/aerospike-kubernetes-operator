package v1beta1

import (
	"context"
	"net/http"

	// "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type mutatingHandler struct {
	// aeroCluster *AerospikeCluster
	decoder *admission.Decoder
}

// InjectDecoder injects the decoder into a mutatingHandler.
func (h *mutatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}

// Handle handles admission requests.
func (h *mutatingHandler) Handle(
	_ context.Context, req admission.Request,
) admission.Response {
	obj := &AerospikeCluster{}
	err := h.decoder.Decode(req, obj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Default the object
	return obj.Default()
}
