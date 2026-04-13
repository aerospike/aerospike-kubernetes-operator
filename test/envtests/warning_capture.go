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

package envtests

import (
	"sync"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WarningCapture implements rest.WarningHandler, collecting all admission-webhook
// warning headers emitted during a single Kubernetes API call.
type WarningCapture struct {
	Warnings []string
	mu       sync.Mutex
}

// HandleWarningHeader implements rest.WarningHandler.
func (w *WarningCapture) HandleWarningHeader(_ int, _, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.Warnings = append(w.Warnings, text)
}

// NewClientWithWarningCapture returns a new Kubernetes client that routes all
// API-server Warning response headers into the returned *WarningCapture.
// Each call produces a fresh capture so callers get per-operation warnings.
func NewClientWithWarningCapture() (client.Client, *WarningCapture, error) {
	capture := &WarningCapture{}
	cfgCopy := rest.CopyConfig(cfg)
	cfgCopy.WarningHandler = capture

	c, err := client.New(cfgCopy, client.Options{Scheme: scheme})

	return c, capture, err
}
