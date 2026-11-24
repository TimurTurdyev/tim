package sweep

import (
	"bufio"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	kinds := map[string]Kind{"none": None, "trunc": None, "addr": Addr, "cname": CNAME, "nodata": NoData, "nx": NX, "refer": Refer, "fail": Fail}
	blank := func(s string) string { return strings.TrimPrefix(s, "-") }
	f, err := os.Open("testdata/replies.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	checked := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		col := strings.Split(line, "\t")
		if len(col) != 6 {
			t.Fatalf("строка образца не из шести колонок: %q", line)
		}
		kind, known := kinds[col[1]]
		msg, err := hex.DecodeString(col[5])
		if err != nil || !known {
			t.Fatalf("образец %s %s не читается: %v", col[0], col[1], err)
		}
		got, truncated := Classify(msg, col[0])
		want := Answer{Name: col[0], Kind: kind, Target: blank(col[3]), Zone: blank(col[4])}
		if got.Name != want.Name || got.Kind != want.Kind || got.Target != want.Target || got.Zone != want.Zone ||
			strings.Join(got.IPs, ",") != blank(col[2]) || truncated != (col[1] == "trunc") {
			t.Errorf("%s, ожидался %s: вид %d, обрезан %v, адреса %v, цель %q, зона %q", col[0], col[1], got.Kind, truncated, got.IPs, got.Target, got.Zone)
		}
		checked++
	}
	if err := sc.Err(); err != nil || checked == 0 {
		t.Fatalf("образцы не прочитаны: %d, %v", checked, err)
	}
}
