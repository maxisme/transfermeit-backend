package main

import (
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
	return err == nil
}