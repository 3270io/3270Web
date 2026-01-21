package host

func WaitUnlockCommandForTest(h *S3270) string {
	if h == nil {
		h = &S3270{}
	}
	return h.waitUnlockCommand()
}
