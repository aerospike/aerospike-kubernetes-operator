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
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var aerospikeClusterLog = logf.Log.WithName("aerospikecluster-resource")

func (c *AerospikeCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()

	aerospikeClusterLog.Info(
		"Registering mutating webhook to the webhook" +
			" server",
	)
	hookServer.Register(
		"/mutate-asdb-aerospike-com-v1-aerospikecluster",
		&webhook.Admission{Handler: &mutatingHandler{}},
	)

	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		Complete()
}
