package cmd

import "testing"

func TestValidAlias(t *testing.T) {
	if validAlias("rm -rf") || validAlias("tim") || !validAlias("gs") {
		t.Error("alias names")
	}
}
