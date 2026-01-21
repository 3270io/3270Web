package host

// WaitUnlockCommandForTest exposes the wait unlock command for unit tests.
// A nil S3270 is acceptable because the command only depends on constants.
func WaitUnlockCommandForTest(h *S3270) string {
	if h == nil {
		h = &S3270{}
	}
	return h.waitUnlockCommand()
}
