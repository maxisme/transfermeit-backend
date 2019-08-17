package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	uuid "github.com/satori/go.uuid"
	"regexp"
	"strings"
)

var validversion = regexp.MustCompile(`^[\d\.]*$`)

func IsValidVersion(version string) bool {
	version = strings.TrimSpace(version)
	if len(version) == 0 {
		return false
	}
	return validversion.MatchString(version)
}

func IsValidUUID(str string) bool {
	_, err := uuid.FromString(str)
	Handle(err)
	return err == nil
}

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
