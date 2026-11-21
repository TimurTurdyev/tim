package ctlog

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	sycamore2027h1 = "jspHC6zeavOiBrCkeoS3Rv4fxr+VPiXmm07kAkjzxug="
	tuscolo2027h1  = "WW5sM4aUsllyolbIoOjdkEp26Ag92oc7AQg4KBQ87lk="
	tiger2027h1    = "HJ9oLOn68EVpUPgbloqH3dsyENhM5siy44JSSsTPWZ8="
)

type mark struct {
	log   string
	ms    int64
	index int64
}

func sample(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sampleHex(t *testing.T, name string) []byte {
	t.Helper()
	data, err := hex.DecodeString(strings.TrimSpace(string(sample(t, name))))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func marksOf(anchors []Anchor) []mark {
	var out []mark
	for _, a := range anchors {
		out = append(out, mark{a.LogID, a.Time.UnixMilli(), a.Index})
	}
	return out
}

// Образцы - расширение 1.3.6.1.4.1.11129.2.4.2 двух настоящих сертификатов: номер записи есть только
// в метке плиточного журнала.
func TestParseSCTs(t *testing.T) {
	cases := []struct {
		file string
		want []mark
	}{
		{"sct-ext-letsencrypt.hex", []mark{
			{sycamore2027h1, 1794686743924, 1211694457},
			{tiger2027h1, 1794686743184, -1},
		}},
		{"sct-ext-globalsign.hex", []mark{
			{"YEyar3p/d18B1Ab8kg3ImesLHH34yVIb+voXdzuXi8k=", 1768309428338, -1},
			{"TGPcmOWcHauI9h6KPd6uj6tEozd7X5uUw/uhnPzBviY=", 1768309428312, -1},
			{sycamore2027h1, 1768309429186, 23095455},
		}},
	}
	for _, c := range cases {
		got, err := parseSCTs(sampleHex(t, c.file))
		if err != nil || !slices.Equal(marksOf(got), c.want) {
			t.Errorf("%s: %+v, ошибка %v; ждали %+v", c.file, marksOf(got), err, c.want)
		}
	}
}

func TestParseSCTsBroken(t *testing.T) {
	whole := sampleHex(t, "sct-ext-letsencrypt.hex")
	// вторая метка обрезана, длины OCTET STRING и списка поправлены под остаток
	cut := slices.Clone(whole[:len(whole)-40])
	cut[2] -= 40
	cut[4] -= 40
	got, err := parseSCTs(cut)
	if err == nil || len(got) != 1 || got[0].Index != 1211694457 {
		t.Errorf("обрезанный список: %+v, ошибка %v; ждали первую метку и ошибку", marksOf(got), err)
	}
	if _, err := parseSCTs([]byte{0x04, 0x02, 0x00, 0x05}); err == nil {
		t.Error("длина списка больше данных, а ошибки нет")
	}
	if _, err := parseSCTs([]byte("not asn1")); err == nil {
		t.Error("мусор разобран без ошибки")
	}
}

func TestAnchors(t *testing.T) {
	notAfter := time.Date(2027, 2, 12, 19, 7, 11, 0, time.UTC)
	cert := &x509.Certificate{
		NotAfter:   notAfter,
		DNSNames:   []string{"*.Example.com", "example.com", "WWW.example.com", "bad\x1b[31mname.example.com"},
		Extensions: []pkix.Extension{{Id: sctOID, Value: sampleHex(t, "sct-ext-letsencrypt.hex")}},
	}
	got := Anchors(cert)
	if len(got) != 2 {
		t.Fatalf("меток %d, ждали 2: %+v", len(got), got)
	}
	names := []string{"example.com", "www.example.com"}
	for _, a := range got {
		if !a.NotAfter.Equal(notAfter) || !slices.Equal(a.Names, names) {
			t.Errorf("метка %s: срок %v, имена %q", a.LogID, a.NotAfter, a.Names)
		}
	}
	if got[0].Index != 1211694457 || got[1].Index != -1 {
		t.Errorf("номера записей %d и %d", got[0].Index, got[1].Index)
	}
	if Anchors(&x509.Certificate{DNSNames: []string{"example.com"}}) != nil {
		t.Error("у сертификата без меток нашлись метки")
	}
}
