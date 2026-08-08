package users

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, following the second option in RFC 9106: 64 MiB of
// memory with a single pass. Memory hardness is what makes a stolen hash
// expensive to attack on GPUs, so it is the parameter to spend on.
//
// These are the parameters used for *new* hashes. Verification reads the
// parameters out of the stored hash, so raising these later keeps old
// passwords working.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

var errMalformedHash = errors.New("users: malformed password hash")

// HashPassword returns a PHC-format Argon2id hash.
//
// The format records the parameters and the salt alongside the digest, so a
// hash stays verifiable after the cost settings change.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("users: generate salt: %w", err)
	}
	digest := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest),
	), nil
}

// VerifyPassword reports whether password matches encoded.
//
// The comparison is constant-time. The work is driven by the parameters stored
// in encoded rather than the current constants, so an old hash costs what it
// originally cost and still verifies.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, digest
	if len(parts) != 6 || parts[0] != "" {
		return false, errMalformedHash
	}
	if parts[1] != "argon2id" {
		return false, fmt.Errorf("users: unsupported password hash algorithm %q", parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, errMalformedHash
	}
	if version != argon2.Version {
		return false, fmt.Errorf("users: unsupported argon2 version %d", version)
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, errMalformedHash
	}
	if memory == 0 || time == 0 || threads == 0 {
		return false, errMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, errMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errMalformedHash
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// decoyHash returns a real hash of a value nobody knows, so a login for a
// user that does not exist can spend the same work as one that does.
//
// Computed once on first use rather than at init, so the cost lands on the
// first failed login instead of on every process start — including the CLI,
// which never logs anybody in.
var decoyHash = sync.OnceValue(func() string {
	password, err := GeneratePassword()
	if err != nil {
		// This is a timing decoy, not a secret; a fixed fallback is fine.
		password = "decoy-password-for-timing-parity"
	}
	hash, err := HashPassword(password)
	if err != nil {
		return ""
	}
	return hash
})
