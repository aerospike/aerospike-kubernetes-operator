package common

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/jsonpatch"
)

// PatchStatus replaces the whole status of obj with status. It sends a JSON patch, not a JSON merge patch: in a
// merge patch a null means "remove the key", so a null that a user set in a raw config in spec would be dropped
// from the copy in status, and spec would never be equal to status again.
func PatchStatus(ctx context.Context, k8sClient client.Client, obj client.Object, status any) error {
	patch, err := json.Marshal([]jsonpatch.PatchOperation{jsonpatch.NewPatch("add", "/status", status)})
	if err != nil {
		return fmt.Errorf("marshal status patch: %w", err)
	}

	return k8sClient.Status().Patch(ctx, obj, client.RawPatch(types.JSONPatchType, patch))
}
