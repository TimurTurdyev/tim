package conv

import (
	"strings"
	"testing"
	"time"
)

func TestParseNumber(t *testing.T) {
	cases := []struct {
		in   string
		base int
		want string
	}{
		{"255", 10, "255"},
		{"ff", 16, "255"},
		{"0xFF", 10, "255"},
		{"0b1010", 10, "10"},
		{"0o17", 16, "15"},
		{"-0x10", 10, "-16"},
		{"1_000", 10, "1000"},
		{"18446744073709551615", 10, "18446744073709551615"},
	}
	for _, c := range cases {
		n, err := ParseNumber(c.in, c.base)
		if err != nil || n.String() != c.want {
			t.Errorf("%s base %d: %v %v", c.in, c.base, n, err)
		}
	}
	for _, bad := range []string{"", "0x", "zz", "12g"} {
		if _, err := ParseNumber(bad, 10); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestHexBin(t *testing.T) {
	n, _ := ParseNumber("4096", 10)
	if Hex(n) != "0x1000" {
		t.Errorf("hex %s", Hex(n))
	}
	if b, ok := Bin(n); !ok || b != "0b0001_0000_0000_0000" {
		t.Errorf("bin %s", b)
	}
	big, _ := ParseNumber("70000", 10)
	if _, ok := Bin(big); ok {
		t.Error("bin over 16 bit")
	}
	neg, _ := ParseNumber("-255", 10)
	if Hex(neg) != "-0xff" {
		t.Errorf("neg hex %s", Hex(neg))
	}
}

func TestBase64Variants(t *testing.T) {
	for _, in := range []string{"aGk/Pz8=", "aGk/Pz8", "aGk_Pz8", "aGk/\nPz8="} {
		out, err := Base64Decode(in)
		if err != nil || string(out) != "hi???" {
			t.Errorf("%q: %q %v", in, out, err)
		}
	}
	if _, err := Base64Decode("@@@"); err == nil {
		t.Error("garbage should fail")
	}
}

// Токен из примера jwt.io с exp в 2018 году.
const sampleJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjE1MTYyNDI2MjJ9.c2lnbmF0dXJl"

func TestDecodeJWT(t *testing.T) {
	j, err := DecodeJWT("Bearer " + sampleJWT)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(j.Header), `"HS256"`) || !strings.Contains(string(j.Claims), `"John Doe"`) {
		t.Errorf("parts: %s %s", j.Header, j.Claims)
	}
	if j.Times["iat"].Unix() != 1516239022 || j.Times["exp"].Unix() != 1516242622 {
		t.Errorf("times: %v", j.Times)
	}
	if expired, ok := j.Expired(time.Unix(1700000000, 0)); !ok || !expired {
		t.Error("should be expired")
	}
	if _, err := DecodeJWT("abc.def"); err == nil {
		t.Error("two parts should fail")
	}
}

func TestParseTime(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	cases := map[string]int64{
		"1700000000":                1700000000,
		"1700000000123":             1700000000,
		"2025-10-18T12:00:00+03:00": 1760778000,
		"2025-10-18 12:00":          1760778000,
		"18.10.2025 12:00":          1760778000,
		"2025-10-18":                1760734800,
	}
	for in, want := range cases {
		got, err := ParseTime(in, msk)
		if err != nil || got.Unix() != want {
			t.Errorf("%s: %v %v want %d", in, got.Unix(), err, want)
		}
	}
	if ms, _ := ParseTime("1700000000123", msk); ms.Nanosecond() != 123000000 {
		t.Errorf("ms lost: %d", ms.Nanosecond())
	}
	if _, err := ParseTime("вчера", msk); err == nil {
		t.Error("should fail")
	}
}

func TestUUIDVersions(t *testing.T) {
	v4, v7 := UUIDv4(), UUIDv7(time.UnixMilli(0x0123456789ab))
	if len(v4) != 36 || v4[14] != '4' || !strings.ContainsRune("89ab", rune(v4[19])) {
		t.Errorf("v4 %s", v4)
	}
	if v7[14] != '7' || !strings.HasPrefix(v7, "01234567-89ab-") {
		t.Errorf("v7 %s", v7)
	}
}
