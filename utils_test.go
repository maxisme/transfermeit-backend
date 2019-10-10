package main

import (
	"fmt"
	"testing"
)

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
