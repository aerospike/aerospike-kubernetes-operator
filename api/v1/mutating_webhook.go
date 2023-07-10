package v1

import (
	"context"
	"net/http"

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
	_ context.Context, req admission.Request, //nolint:gocritic // used by external lib
) admission.Response {
	obj := &AerospikeCluster{}

	if err := h.decoder.Decode(req, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Default the object
	return obj.Default(req.Operation)
}
