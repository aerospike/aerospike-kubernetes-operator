/*
Copyright 2021.

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

package v1

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// WebhookOption configures the AerospikeCluster webhook.
type WebhookOption func(*webhookOptions)

type webhookOptions struct {
	nodeIPv6CacheTTL time.Duration
}

// WithNodeIPv6CacheTTL overrides how long the node IPv6 capability probe result is reused.
// Tests use a short TTL so a seeded node is observed promptly; production uses the default.
func WithNodeIPv6CacheTTL(ttl time.Duration) WebhookOption {
	return func(o *webhookOptions) {
		o.nodeIPv6CacheTTL = ttl
	}
}

// SetupAerospikeClusterWebhookWithManager registers the webhook for AerospikeCluster in the manager.
func SetupAerospikeClusterWebhookWithManager(mgr ctrl.Manager, opts ...WebhookOption) error {
	var options webhookOptions

	for _, opt := range opts {
		opt(&options)
	}

	// The prober reads through the API reader rather than the manager cache: nodes are
	// read rarely and behind a cache of their own, so a cluster-wide node informer would
	// cost memory and a watch permission for nothing.
	validator := &AerospikeClusterCustomValidator{
		ipv6Prober: newNodeIPv6Prober(mgr.GetAPIReader(), options.nodeIPv6CacheTTL),
	}

	return ctrl.NewWebhookManagedBy(mgr, &asdbv1.AerospikeCluster{}).
		WithDefaulter(&AerospikeClusterCustomDefaulter{}).
		WithValidator(validator).
		Complete()
}
