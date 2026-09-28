package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	minimumPasswordLength = 12
	maximumPasswordLength = 128

	argon2Memory      = 64 * 1024
	argon2Iterations  = 3
	argon2Parallelism = 2
	argon2SaltLength  = 16
	argon2KeyLength   = 32
)

var errInvalidPasswordHash = errors.New("invalid password hash")

// hashPassword produces a PHC-style Argon2id string. The random salt is
// embedded in the encoded value, so the database only needs one hash field.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2Memory,
		argon2Iterations,
		argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// verifyPassword also accepts legacy bcrypt hashes. A successful bcrypt
// verification requests an upgrade so accounts migrate without a reset.
func verifyPassword(encodedHash, password string) (valid, needsUpgrade bool) {
	if strings.HasPrefix(encodedHash, "$2a$") || strings.HasPrefix(encodedHash, "$2b$") || strings.HasPrefix(encodedHash, "$2y$") {
		valid := bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password)) == nil
		return valid, valid
	}

	params, salt, expected, err := decodeArgon2idHash(encodedHash)
	if err != nil {
		return false, false
	}
	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(expected)))
	valid = subtle.ConstantTimeCompare(actual, expected) == 1
	needsUpgrade = valid && (params.memory != argon2Memory || params.iterations != argon2Iterations || params.parallelism != argon2Parallelism || len(expected) != argon2KeyLength)
	return valid, needsUpgrade
}

type argon2idParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func decodeArgon2idHash(encoded string) (argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2idParams{}, nil, nil, errInvalidPasswordHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version || parts[2] != fmt.Sprintf("v=%d", version) {
		return argon2idParams{}, nil, nil, errInvalidPasswordHash
	}
	var params argon2idParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.iterations, &params.parallelism); err != nil || parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", params.memory, params.iterations, params.parallelism) {
		return argon2idParams{}, nil, nil, errInvalidPasswordHash
	}
	// Bound parameters before allocating memory, even if the database has been tampered with.
	if params.memory < 8*1024 || params.memory > 256*1024 || params.iterations < 1 || params.iterations > 10 || params.parallelism < 1 || params.parallelism > 16 {
		return argon2idParams{}, nil, nil, errInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return argon2idParams{}, nil, nil, errInvalidPasswordHash
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(hash) < 16 || len(hash) > 64 {
		return argon2idParams{}, nil, nil, errInvalidPasswordHash
	}
	return params, salt, hash, nil
}

func passwordIsAcceptable(password string) bool {
	length := utf8.RuneCountInString(password)
	return utf8.ValidString(password) && length >= minimumPasswordLength && length <= maximumPasswordLength
}
