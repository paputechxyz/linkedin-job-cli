package cmd

import (
	"strings"
	"testing"
)

func TestReadPasted(t *testing.T) {
	got, err := readPasted(strings.NewReader("line1\nline2\n"))
	if err != nil {
		t.Fatalf("readPasted: %v", err)
	}
	if got != "line1\nline2" {
		t.Errorf("got %q, want %q", got, "line1\nline2")
	}
}

func TestReadPasted_EmptyYieldsEmpty(t *testing.T) {
	got, _ := readPasted(strings.NewReader(""))
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
