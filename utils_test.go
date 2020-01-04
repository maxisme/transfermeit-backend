package main

import (
	"fmt"
	"testing"
)

// CreditToFileUploadSize()
var creditToBytes = []struct {
	credit float64
	bytes  int
}{
	{0.0, FreeFileUploadBytes},
	{5.0, MegabytesToBytes(2750)},
	{7.5, MegabytesToBytes(4000)},
}

func TestCreditToFileUploadSize(t *testing.T) {
	for _, tt := range creditToBytes {
		t.Run(fmt.Sprintf("%f", tt.credit), func(t *testing.T) {
			v := CreditToFileUploadSize(float64(tt.credit))
			if v != tt.bytes {
				t.Errorf("got %v, wanted %v", v, tt.bytes)
			}
		})
	}
}

// CreditToBandwidth()
var creditToBandwidth = []struct {
	credit    float64
	bandwidth int
}{
	{0.0, FreeBandwidthBytes},
	{5.0, MegabytesToBytes(27500)},
	{7.5, MegabytesToBytes(40000)},
}

func TestCreditToBandwidth(t *testing.T) {
	for _, tt := range creditToBandwidth {
		t.Run(fmt.Sprintf("%f", tt.credit), func(t *testing.T) {
			v := CreditToBandwidth(float64(tt.credit))
			if v != tt.bandwidth {
				t.Errorf("got %v, wanted %v", v, tt.bandwidth)
			}
		})
	}
}

// BytesToReadable()
var bytesToReadable = []struct {
	bytes    int
	readable string
}{
	{0, "0 bytes"},
	{1, "1 bytes"},
	{MegabytesToBytes(8000), "8 GB"},
	{MegabytesToBytes(1), "1 MB"},
}

func TestBytesToReadable(t *testing.T) {
	for _, tt := range bytesToReadable {
		t.Run(fmt.Sprintf("%d", tt.bytes), func(t *testing.T) {
			v := BytesToReadable(tt.bytes)
			if v != tt.readable {
				t.Errorf("got %v, wanted %v", v, tt.readable)
			}
		})
	}
}
