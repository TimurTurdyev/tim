// Package localca: свой удостоверяющий центр для локальной разработки и выпуск сертификатов им.
package localca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// LeafDays: срок жизни выпускаемого сертификата. Apple ограничивает публично доверенные
// сертификаты 398 днями, к своему CA это не относится, но и вечный срок заводить незачем.
const LeafDays = 825

const (
	certFile = "rootCA.pem"
	keyFile  = "rootCA-key.pem"
)

// Root: папка с ключом и сертификатом CA.
func Root() string {
	if d := os.Getenv("TIM_CAROOT"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".tim-ca"
	}
	return filepath.Join(home, ".local", "share", "tim", "ca")
}

func CertPath() string { return filepath.Join(Root(), certFile) }
func KeyPath() string  { return filepath.Join(Root(), keyFile) }

// CA: сертификат удостоверяющего центра и его ключ.
type CA struct {
	Cert *x509.Certificate
	Key  *ecdsa.PrivateKey
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}

// Load читает CA с диска.
func Load() (*CA, error) {
	certPEM, err := os.ReadFile(CertPath())
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(KeyPath())
	if err != nil {
		return nil, err
	}
	cert, err := parseCert(certPEM)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, os.ErrInvalid
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, Key: key}, nil
}

func parseCert(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, os.ErrInvalid
	}
	return x509.ParseCertificate(block.Bytes)
}

// Name: кому принадлежит CA, чтобы в списке хранилища его было видно.
func Name() string {
	who := "tim"
	if u, err := user.Current(); err == nil && u.Username != "" {
		who = u.Username
	}
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}
	return "tim local CA " + who + "@" + strings.TrimSuffix(host, ".local")
}

// Create создаёт новый CA на 10 лет и кладёт его в Root().
func Create() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          sn,
		Subject:               pkix.Name{CommonName: Name(), Organization: []string{"tim local CA"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(Root(), 0o755); err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writePEM(KeyPath(), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return nil, err
	}
	if err := writePEM(CertPath(), "CERTIFICATE", der, 0o644); err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, Key: key}, nil
}

// LoadOrCreate: CA с диска, а если его там нет - новый. Второй результат - был ли он создан сейчас.
func LoadOrCreate() (*CA, bool, error) {
	ca, err := Load()
	if err == nil {
		return ca, false, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, err
	}
	ca, err = Create()
	return ca, true, err
}

func writePEM(path, kind string, der []byte, mode os.FileMode) error {
	data := pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der})
	return os.WriteFile(path, data, mode)
}

// Names: что положить в SAN. DNS-имена и адреса различаются сами.
// Wildcard не покрывает ни сам домен, ни второй уровень, поэтому к каждому имени
// добавляется "*.имя", если только это не отключено.
func Names(args []string, exact bool) (dns []string, ips []net.IP) {
	seen := map[string]bool{}
	addDNS := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			dns = append(dns, n)
		}
	}
	for _, a := range args {
		a = strings.TrimSpace(strings.TrimSuffix(a, "."))
		if a == "" {
			continue
		}
		if ip := net.ParseIP(a); ip != nil {
			ips = append(ips, ip)
			continue
		}
		addDNS(a)
		if !exact && !strings.HasPrefix(a, "*.") && strings.Contains(a, ".") {
			addDNS("*." + a)
		}
	}
	return dns, ips
}

// Leaf: выпущенный сертификат вместе с ключом, оба в PEM.
type Leaf struct {
	Cert    []byte
	Key     []byte
	DNS     []string
	IPs     []net.IP
	Expires time.Time
}

// Issue выпускает сертификат на перечисленные имена, подписанный ca.
func (ca *CA) Issue(dns []string, ips []net.IP, days int) (*Leaf, error) {
	if len(dns) == 0 && len(ips) == 0 {
		return nil, os.ErrInvalid
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	cn := "tim local"
	if len(dns) > 0 {
		cn = dns[0]
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"tim local development"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(0, 0, days),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &Leaf{
		Cert:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		Key:     pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		DNS:     dns,
		IPs:     ips,
		Expires: tmpl.NotAfter,
	}, nil
}

// FileStem: имя файлов сертификата по первому имени в нём.
func FileStem(dns []string, ips []net.IP) string {
	if len(dns) > 0 {
		return strings.ReplaceAll(strings.TrimPrefix(dns[0], "*."), "*", "_")
	}
	if len(ips) > 0 {
		return strings.ReplaceAll(ips[0].String(), ":", "_")
	}
	return "local"
}

// LeafPaths: где лежат сертификат и ключ, выпущенные для имени через tim link.
func LeafPaths(name string) (cert, key string) {
	dir := filepath.Join(filepath.Dir(Root()), "certs")
	stem := FileStem([]string{name}, nil)
	return filepath.Join(dir, stem+".pem"), filepath.Join(dir, stem+"-key.pem")
}

// Save кладёт сертификат и ключ по путям, создавая папку. Ключ доступен только владельцу.
func (l *Leaf) Save(certPath, keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(certPath, l.Cert, 0o644); err != nil {
		return err
	}
	return os.WriteFile(keyPath, l.Key, 0o600)
}
