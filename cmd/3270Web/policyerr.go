package main

// Refusals that are decisions rather than failures.
//
// A connection can fail because the host did not answer, or be refused
// because this instance will never dial it, or because the caller has spent
// their allowance. All three used to arrive at the caller as one error and
// one status, which told a client to retry something that will never succeed.

// policyError carries a message written for the person reading it, and a
// sentinel for code deciding what to do about it.
//
// Wrapping with fmt.Errorf("%w: ...") would have been shorter and puts the
// sentinel's own text in front of the explanation — "host not allowed: this
// 3270Web instance is not permitted to connect to ...". The reason and the
// message are different audiences, so they are different fields.
type policyError struct {
	reason  error
	message string
}

func (e policyError) Error() string { return e.message }
func (e policyError) Unwrap() error { return e.reason }

// refuse builds a policyError.
func refuse(reason error, message string) error {
	return policyError{reason: reason, message: message}
}
