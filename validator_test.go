package main

import "testing"

func TestIsValidUUID(t *testing.T) {
	if IsValidUUID("62b5873e-71bf-4659-af9d796581f126f8") {
		t.Errorf("Should be invalid UUID")
	}

	if !IsValidUUID("BB8C9950-286C-5462-885C-0CFED585423B") {
		t.Errorf("Should be valid UUID")
	}
}

var versiontests = []struct {
	in  string
	out bool
}{
	{"", false},
	{" ", false},
	{"1", true},
	{"a", false},
	{"1.", true},
	{"1.2", true},
	{"1.3.4", true},
	{"1.34234244", true},
	{"1.2aa2.3a", false},
	{"1.a.3", false},
}

func TestIsValidVersion(t *testing.T) {
	for _, tt := range versiontests {
		t.Run(tt.in, func(t *testing.T) {
			v := IsValidVersion(tt.in)
			if v != tt.out {
				t.Errorf("got %v, wanted %v", v, tt.out)
			}
		})
	}
}

var keys = []struct {
	key        string
	shouldFail bool
}{
	{"", true},
	{" ", true},
	{"MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvxvSoA5+YJ0dK3HFy9ccnalbqSgVGJYmQXl/1JBcN1zZGUrsBDAPRdX+TTgWbW4Ah8C+PUVmf6YbA5d+ZWmBUIYds4Ft/v2qbh3/rBEFvNw+/HhspclzwI1On6EcnylLalpF6JYYjuw4QqIJd/CsnABZwAFQ8czdtUbomic7gh9UdjkEFed5C3QqD3Nes7w7glkrEocTzwizLuxnpQZFhDEjGgONgGJSi92yf8eh0STSLGrWjT8+nw/Dw6RSWQAZviEyRtJ52WdFHIsQEAU81N5NpCr7rDPr9GHFU8sdo8Lp3fQntOIvyjpIzKUXWyp+QVJAh6GMw2Fn16S+Jg127wIDAQAB", false},
	{"aMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvxvSoA5+YJ0dK3HFy9ccnalbqSgVGJYmQXl/1JBcN1zZGUrsBDAPRdX+TTgWbW4Ah8C+PUVmf6YbA5d+ZWmBUIYds4Ft/v2qbh3/rBEFvNw+/HhspclzwI1On6EcnylLalpF6JYYjuw4QqIJd/CsnABZwAFQ8czdtUbomic7gh9UdjkEFed5C3QqD3Nes7w7glkrEocTzwizLuxnpQZFhDEjGgONgGJSi92yf8eh0STSLGrWjT8+nw/Dw6RSWQAZviEyRtJ52WdFHIsQEAU81N5NpCr7rDPr9GHFU8sdo8Lp3fQntOIvyjpIzKUXWyp+QVJAh6GMw2Fn16S+Jg127wIDAQAB", true},
}

func TestIsValidPublicKey(t *testing.T) {
	for _, tt := range keys {
		t.Run(tt.key, func(t *testing.T) {
			v := IsValidPublicKey(tt.key)
			failed := false
			if v != nil {
				failed = true
			}
			if failed != tt.shouldFail {
				t.Errorf("got %v, wanted %v", v, tt.shouldFail)
			}
		})
	}
}
