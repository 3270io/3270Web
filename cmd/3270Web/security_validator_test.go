package main

import "testing"

func TestValidator_RejectsRestrictedIPs(t *testing.T) {
	// Verify that user input does NOT allow access to link-local addresses
	// including Cloud Metadata Service (169.254.169.254)
	cloudMetadata := "169.254.169.254"
	ipv6LinkLocal := "fe80::1"

	if isValidHostname(cloudMetadata) {
		t.Fatalf("Expected %q to be REJECTED (SSRF protection), but it was valid.", cloudMetadata)
	}
	if isValidHostname(ipv6LinkLocal) {
		t.Fatalf("Expected %q to be REJECTED (SSRF protection), but it was valid.", ipv6LinkLocal)
	}
}
