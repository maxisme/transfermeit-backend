package main

import (
	"fmt"
	"testing"
)

var encryptedBytes = []struct {
	in  int
	out int
}{
	{5, 82},
	{51324, 51394},
	{4, 82},
	{16, 98},
	{32, 114},
	{21314354332, 21314354402},
}

func TestFileSizeToRNCryptorBytes(t *testing.T) {
	for _, tt := range encryptedBytes {
		t.Run(string(tt.in), func(t *testing.T) {
			v := FileSizeToRNCryptorBytes(tt.in)
			if v != tt.out {
				t.Errorf("got %v, wanted %v", v, tt.out)
			}
		})
	}
}

var creditToBytes = []struct {
	in  float64
	out int
}{
	{5.0, MegabytesToBytes(2750)},
	{7.5, MegabytesToBytes(4000)},
}

func TestCreditToFileUpload(t *testing.T) {
	for _, tt := range creditToBytes {
		t.Run(fmt.Sprintf("%f", tt.in), func(t *testing.T) {
			v := CreditToFileUpload(float64(tt.in))
			if v != tt.out {
				t.Errorf("got %v, wanted %v", v, tt.out)
			}
		})
	}
}
