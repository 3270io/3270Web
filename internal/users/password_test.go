package users

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=") {
		t.Errorf("hash %q is not PHC-format argon2id", hash)
	}
	if strings.Contains(hash, testPassword) {
		t.Fatal("the hash contains the password")
	}

	ok, err := VerifyPassword(hash, testPassword)
	if err != nil || !ok {
		t.Errorf("VerifyPassword(correct) = %v, %v; want true, nil", ok, err)
	}

	ok, err = VerifyPassword(hash, testPassword+"x")
	if err != nil {
		t.Errorf("VerifyPassword(wrong) errored: %v", err)
	}
	if ok {
		t.Error("a wrong password verified")
	}
}

// A fresh salt per hash is what stops two identical passwords from being
// visibly identical in the store.
func TestHashIsSaltedPerCall(t *testing.T) {
	a, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("hashing the same password twice produced the same string")
	}

	for _, h := range []string{a, b} {
		ok, err := VerifyPassword(h, testPassword)
		if err != nil || !ok {
			t.Errorf("hash %q did not verify: %v, %v", h, ok, err)
		}
	}
}

// Verification reads its cost parameters from the stored hash, so raising the
// defaults later must not lock existing users out.
func TestVerifyHonoursStoredParameters(t *testing.T) {
	// A hash built with parameters deliberately unlike the current defaults.
	const (
		otherTime    = 2
		otherMemory  = 8 * 1024
		otherThreads = 1
	)
	salt := []byte("0123456789abcdef")
	digest := argon2.IDKey([]byte(testPassword), salt, otherTime, otherMemory, otherThreads, 32)
	encoded := encodePHC(otherMemory, otherTime, otherThreads, salt, digest)

	ok, err := VerifyPassword(encoded, testPassword)
	if err != nil {
		t.Fatalf("VerifyPassword on non-default parameters: %v", err)
	}
	if !ok {
		t.Error("a hash made with different parameters failed to verify")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, bad := range []string{
		"",
		"not-a-hash",
		"$argon2id$",
		"$argon2id$v=19$m=65536,t=1,p=4$onlyfourfields",
		"$argon2id$v=19$m=65536,t=1,p=4$!!!notbase64!!!$aaaa",
		"$argon2id$v=19$m=65536,t=1,p=4$aaaa$!!!notbase64!!!",
		"$argon2id$v=19$m=0,t=0,p=0$YWFhYQ$YWFhYQ",
		"$argon2id$v=999$m=65536,t=1,p=4$YWFhYQ$YWFhYQ",
		// A different algorithm must be refused rather than silently treated
		// as argon2id.
		"$argon2i$v=19$m=65536,t=1,p=4$YWFhYQ$YWFhYQ",
		"$bcrypt$v=19$m=65536,t=1,p=4$YWFhYQ$YWFhYQ",
	} {
		ok, err := VerifyPassword(bad, testPassword)
		if ok {
			t.Errorf("VerifyPassword(%q) returned true", bad)
		}
		if err == nil {
			t.Errorf("VerifyPassword(%q) returned no error", bad)
		}
	}
}

func TestDecoyHashIsUsable(t *testing.T) {
	h := decoyHash()
	if h == "" {
		t.Fatal("decoy hash is empty, so a login for an unknown user does no work")
	}
	ok, err := VerifyPassword(h, "anything at all")
	if err != nil {
		t.Errorf("the decoy hash is malformed: %v", err)
	}
	if ok {
		t.Error("the decoy hash matched a guess")
	}
	if decoyHash() != h {
		t.Error("decoyHash is not memoised")
	}
}

func encodePHC(memory, time uint32, threads uint8, salt, digest []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, time, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest),
	)
}
