package main

import (
	"fmt"
	"testing"
)

// CreditToFileUploadSize()
var creditToBytes = []struct {
	credit float64
	bytes  int64
}{
	{0.0, freeFileUploadBytes},
	{5.0, MegabytesToBytes(2750)},
	{7.5, MegabytesToBytes(4000)},
}

func TestCreditToFileUploadSize(t *testing.T) {
	for _, tt := range creditToBytes {
		t.Run(fmt.Sprintf("%f", tt.credit), func(t *testing.T) {
			v := CreditToFileUploadSize(tt.credit)
			if v != tt.bytes {
				t.Errorf("got %v, wanted %v", v, tt.bytes)
			}
		})
	}
}

// CreditToBandwidth()
var creditToBandwidth = []struct {
	credit    float64
	bandwidth int64
}{
	{0.0, freeBandwidthBytes},
	{5.0, MegabytesToBytes(27500)},
	{7.5, MegabytesToBytes(40000)},
}

func TestCreditToBandwidth(t *testing.T) {
	for _, tt := range creditToBandwidth {
		t.Run(fmt.Sprintf("%f", tt.credit), func(t *testing.T) {
			v := CreditToBandwidth(tt.credit)
			if v != tt.bandwidth {
				t.Errorf("got %v, wanted %v", v, tt.bandwidth)
			}
		})
	}
}

// BytesToReadable()
var bytesToReadable = []struct {
	bytes    int64
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

func TestBytesToMegabytes(t *testing.T) {
	MB := BytesToMegabytes(10000)
	if MB != 0.01 {
		t.Errorf("got %f, wanted %f", MB, 0.01)
	}
}

func TestUpdateErr(t *testing.T) {
	// invalid UUID so shouldn't update any rows
	err := UpdateErr(s.db.Exec(`
	UPDATE user 
	SET UUID_key=''
	WHERE UUID = ?
	`, "notauuid"))
	if err == nil {
		t.Errorf("Should have failed because there will never be a uuid value of 'notauuid'")
	}

	// should fail because invalid SQL is passed
	err = UpdateErr(s.db.Exec(`NOT valid SQL`))
	if err == nil {
		t.Errorf("Should have failed because of invalid SQL")
	}
}
