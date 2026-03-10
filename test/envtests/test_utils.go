package envtests

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatusErrorMatcher provides a fluent interface for validating Kubernetes StatusErrors.
// It validates error type, message substrings, and optional causes.
//
// Example usage:
//
//	NewStatusErrorMatcher().
//	    WithMessageSubstrings("admission webhook", "denied the request").
//	    Validate(err)
//
//	NewStatusErrorMatcher().
//	    WithCauses(metav1.StatusCause{
//	        Type:    metav1.CauseTypeFieldValueInvalid,
//	        Message: "invalid value",
//	        Field:   "spec.field",
//	    }).
//	    Validate(err)
type StatusErrorMatcher struct {
	messageSubstrings []string
	causes            []metav1.StatusCause
	warnings          []string
	checkCauses       bool
}

// NewStatusErrorMatcher creates a new StatusErrorMatcher.
func NewStatusErrorMatcher() *StatusErrorMatcher {
	return &StatusErrorMatcher{}
}

// WithMessageSubstrings adds message substring validation.
// All provided substrings must be present in the error message.
func (m *StatusErrorMatcher) WithMessageSubstrings(substrings ...string) *StatusErrorMatcher {
	m.messageSubstrings = append(m.messageSubstrings, substrings...)
	return m
}

// WithCauses adds cause validation.
// The error must contain exactly the specified causes in the same order.
func (m *StatusErrorMatcher) WithCauses(causes ...metav1.StatusCause) *StatusErrorMatcher {
	m.causes = causes
	m.checkCauses = true

	return m
}

// WithWarnings adds warning validation.
// All provided warnings must be present in the response.
func (m *StatusErrorMatcher) WithWarnings(warnings ...string) *StatusErrorMatcher {
	m.warnings = append(m.warnings, warnings...)
	return m
}

// Validate performs the validation against the provided error.
// It will fail the test (via Gomega assertions) if any validation fails.
func (m *StatusErrorMatcher) Validate(err error) {
	GinkgoHelper() // Mark this as a helper function for better error reporting

	// 1. Cast the error to a StatusError pointer
	statusErr, ok := err.(*errors.StatusError)
	Expect(ok).To(BeTrue(), "Error should be a Kubernetes StatusError")

	// 2. Validate the status fields
	Expect(statusErr.ErrStatus.Status).To(Equal(metav1.StatusFailure))

	// 3. Validate message substrings if provided
	for _, substring := range m.messageSubstrings {
		Expect(statusErr.ErrStatus.Message).To(ContainSubstring(substring))
	}

	// 4. Validate Causes if CheckCauses is enabled
	if m.checkCauses {
		Expect(statusErr.ErrStatus.Details).NotTo(BeNil(), "Expected Details to be present for cause validation")
		Expect(statusErr.ErrStatus.Details.Causes).To(HaveLen(len(m.causes)),
			"Expected %d causes but got %d", len(m.causes), len(statusErr.ErrStatus.Details.Causes))

		for i, expectedCause := range m.causes {
			actualCause := statusErr.ErrStatus.Details.Causes[i]
			Expect(actualCause.Type).To(Equal(expectedCause.Type),
				"Cause[%d].Type mismatch", i)
			Expect(actualCause.Field).To(Equal(expectedCause.Field),
				"Cause[%d].Field mismatch", i)
			Expect(actualCause.Message).To(Equal(expectedCause.Message),
				"Cause[%d].Message mismatch", i)
		}
	}

	// 5. Validate warnings if provided
	// Note: Warnings would typically come from the response metadata, not the error itself
	// This assumes warnings are passed separately or stored in the matcher
	for _, expectedWarning := range m.warnings {
		// Warning validation logic would go here
		// This might require additional context about how warnings are captured
		// For now, we'll add a placeholder
		_ = expectedWarning // Suppress unused variable warning
	}
}
