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

// Package envtests: test env setup and helpers for subpackages (e.g. test/envtests/cluster).
// This file has no _test suffix so the package can be imported by other test packages.
package envtests

import (
	"strings"

	//nolint:staticcheck // ST1001: dot imports are standard practice for Gomega in test helpers
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// K8sClient is the Kubernetes client for envtests. Set by SetupTestEnv or by the parent suite.
var K8sClient client.Client

// StatusErrorMatcher validates a Kubernetes API StatusError (e.g. from admission webhooks).
type StatusErrorMatcher struct {
	reason            metav1.StatusReason
	messageSubstrings []string
	causes            []metav1.StatusCause
	code              int32
}

// NewStatusErrorMatcher returns a matcher expecting the given status code and reason.
func NewStatusErrorMatcher(code int32, reason metav1.StatusReason) *StatusErrorMatcher {
	return &StatusErrorMatcher{code: code, reason: reason}
}

// WithMessageSubstrings adds required substrings that must appear in the status message.
func (m *StatusErrorMatcher) WithMessageSubstrings(ss ...string) *StatusErrorMatcher {
	m.messageSubstrings = append(m.messageSubstrings, ss...)
	return m
}

// WithCauses adds expected status causes to match.
func (m *StatusErrorMatcher) WithCauses(causes ...metav1.StatusCause) *StatusErrorMatcher {
	m.causes = append(m.causes, causes...)
	return m
}

// Validate asserts that err is a StatusError matching code, reason, message substrings, and causes.
func (m *StatusErrorMatcher) Validate(err error) {
	statusErr, ok := err.(*apierrors.StatusError)
	Expect(ok).To(BeTrue(), "expected a *errors.StatusError, got %T", err)
	Expect(statusErr.ErrStatus.Code).To(Equal(m.code))
	Expect(statusErr.ErrStatus.Reason).To(Equal(m.reason))

	msg := statusErr.ErrStatus.Message
	for _, sub := range m.messageSubstrings {
		Expect(strings.Contains(msg, sub)).To(BeTrue(), "message %q should contain %q", msg, sub)
	}

	if len(m.causes) > 0 {
		Expect(statusErr.ErrStatus.Details).NotTo(BeNil())
		Expect(statusErr.ErrStatus.Details.Causes).To(HaveLen(len(m.causes)))

		for i, c := range m.causes {
			Expect(statusErr.ErrStatus.Details.Causes[i].Type).To(Equal(c.Type))
			Expect(statusErr.ErrStatus.Details.Causes[i].Message).To(Equal(c.Message))
			Expect(statusErr.ErrStatus.Details.Causes[i].Field).To(Equal(c.Field))
		}
	}
}
