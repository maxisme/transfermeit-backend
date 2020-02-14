package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	uuid "github.com/satori/go.uuid"
	"regexp"
	"strings"
)

// ValidVersionRegex is regex to match a valid version
var ValidVersionRegex = regexp.MustCompile(`^[\d\.]*$`)

// IsValidVersion checks if a string is in the format of a valid version
func IsValidVersion(version string) bool {
	version = strings.TrimSpace(version)
	if len(version) == 0 {
		return false
	}
	return ValidVersionRegex.MatchString(version)
}

// IsValidUUID checks if a string is a UUID
func IsValidUUID(str string) bool {
	_, err := uuid.FromString(str)
	return err == nil
}

// IsValidPublicKey checks if a string is a valid public key
func IsValidPublicKey(pubKey string) bool {
	decodedPublicKey, err := base64.StdEncoding.DecodeString(pubKey)
	Handle(err)
	if err != nil {
		return false
	}

	re, err := x509.ParsePKIXPublicKey(decodedPublicKey)
	Handle(err)
	if err != nil {
		return false
	}

	pub := re.(*rsa.PublicKey)
	if pub == nil {
		return false
	}
	return true
}
