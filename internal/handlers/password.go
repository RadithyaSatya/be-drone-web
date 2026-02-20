package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func verifyPassword(password, stored string) (bool, error) {
	stored = strings.TrimSpace(stored)
	switch {
	case strings.HasPrefix(stored, "sha256:"):
		return verifySHA256Password(password, stored)
	case strings.HasPrefix(stored, "$2"):
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
		if err == bcrypt.ErrMismatchedHashAndPassword {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, errors.New("unsupported password hash format")
	}
}

func validatePasswordHash(stored string) error {
	stored = strings.TrimSpace(stored)
	switch {
	case strings.HasPrefix(stored, "sha256:"):
		_, _, err := parseSHA256PasswordHash(stored)
		return err
	case strings.HasPrefix(stored, "$2"):
		return nil
	default:
		return errors.New("unsupported password hash format")
	}
}

func verifySHA256Password(password, stored string) (bool, error) {
	salt, expected, err := parseSHA256PasswordHash(stored)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(append(salt, []byte(password)...))
	if subtle.ConstantTimeCompare(sum[:], expected) == 1 {
		return true, nil
	}
	return false, nil
}

func parseSHA256PasswordHash(stored string) ([]byte, []byte, error) {
	parts := strings.SplitN(stored, ":", 3)
	if len(parts) != 3 || parts[0] != "sha256" {
		return nil, nil, errors.New("invalid sha256 password hash format")
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, nil, errors.New("invalid sha256 salt")
	}
	expected, err := hex.DecodeString(parts[2])
	if err != nil {
		return nil, nil, errors.New("invalid sha256 hash")
	}
	if len(expected) != sha256.Size {
		return nil, nil, errors.New("invalid sha256 hash length")
	}
	return salt, expected, nil
}
