package secrets

import "testing"

func TestParseHistory(t *testing.T) {
	out := `@@c3d4e5f 02.05.2025
config/app.php
.env.example

@@a1b2c3d 14.03.2025
.env
storage/dump.sql.gz
tests/fixtures/server.key
docker/.env.testing
@@0aa0aa0 01.01.2025
.env
`
	got := parseHistory(out)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	// .env добавлялся дважды: важен первый раз, самый старый коммит.
	if got[0].Where != ".env" || got[0].Commit != "0aa0aa0 01.01.2025" || got[1].What != "dump.sql.gz" {
		t.Fatalf("%+v", got)
	}
}
