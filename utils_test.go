package main

import "testing"

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
