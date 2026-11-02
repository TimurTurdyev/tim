package diskuse

import (
	"os"
	"testing"
)

func TestDockerReclaim(t *testing.T) {
	out, err := os.ReadFile("testdata/docker-system-df.txt")
	if err != nil {
		t.Fatal(err)
	}
	prune, volumes := DockerReclaim(string(out))
	if prune != 3_214_000_000+12_500+1_632_000_000 || volumes != 733_700_000 {
		t.Fatalf("prune %d, volumes %d", prune, volumes)
	}
}
