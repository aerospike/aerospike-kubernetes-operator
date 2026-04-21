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

// WarningCapture implements rest.WarningHandler, collecting all API-server
// Warning response headers emitted during Kubernetes API calls.
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

// Reset clears all captured warnings. Call this in BeforeEach before each
// operation whose warnings need to be inspected in isolation.
func (w *WarningCapture) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.Warnings = nil
}

// GlobalWarnings collects API-server Warning headers from WarningK8sClient.
// Call GlobalWarnings.Reset() in BeforeEach to get per-test warnings.
//
// NOTE: Not safe for parallel tests. If specs run concurrently, a BeforeEach
// Reset() in one test can wipe warnings that another test is still capturing,
// and warnings from different tests can bleed into each other's assertions.
var GlobalWarnings = &WarningCapture{}

// WarningK8sClient is a Kubernetes client that routes all API-server Warning
// response headers into GlobalWarnings.
var WarningK8sClient client.Client

// initWarningClient creates WarningK8sClient backed by GlobalWarnings.
func initWarningClient() error {
	cfgCopy := rest.CopyConfig(cfg)
	cfgCopy.WarningHandler = GlobalWarnings

	var err error

	WarningK8sClient, err = client.New(cfgCopy, client.Options{Scheme: scheme})

	return err
}
