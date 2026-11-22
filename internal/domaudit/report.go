package domaudit

import (
	"crypto/x509"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/internetdb"
	"github.com/TimurTurdyev/tim/internal/portscan"
)

// Host: найденное имя и что о нём известно.
type Host struct {
	Name   string   `json:"name"`
	IPs    []string `json:"ips,omitempty"`
	Status int      `json:"status,omitempty"`
	Scheme string   `json:"scheme,omitempty"`
	Server string   `json:"server,omitempty"`
	Title  string   `json:"title,omitempty"`
	Engine string   `json:"engine,omitempty"` // движок сайта: WordPress, Bitrix
	Final  string   `json:"final,omitempty"`
	Source string   `json:"source"`
	// Sources: все источники, которые знают имя.
	Sources []string `json:"sources,omitempty"`
	// CNAME: куда ведёт имя; Takeover - его можно захватить: цель у чужого сервиса никем не занята.
	CNAME    string    `json:"cname,omitempty"`
	Takeover *Takeover `json:"takeover,omitempty"`
	// CertError: почему браузер не примет сертификат имени; CertName - на какое имя он выдан.
	CertError string `json:"cert_error,omitempty"`
	CertName  string `json:"cert_name,omitempty"`
	// Internal: все адреса имени внутренние - в интернете за ним никого нет.
	Internal bool `json:"internal,omitempty"`
	// Unchecked: имя за пределом сбора, по HTTP его не спрашивали.
	Unchecked bool `json:"unchecked,omitempty"`

	trackers  []Tracker         // только у главной страницы
	certNames []string          // все имена сертификата
	pageNames []string          // имена домена со страницы и из заголовков ответа
	cert      *x509.Certificate // сертификат сайта: по его меткам ищутся соседи в журналах
}

// Silent: имя спрашивали по HTTP, и оно не ответило.
func (h Host) Silent() bool { return h.Status == 0 && !h.Unchecked && !h.Internal }

// Addr: адрес, его сеть и кто на него смотрит: имена, почтовые серверы и серверы имён домена. CDN - узел
// раздачи, настоящий сервер за ним не виден; Own - сервер самого домена: имя ведёт сюда без CNAME на чужой
// сервис; Internal - адрес не из интернета.
type Addr struct {
	IP       string     `json:"ip"`
	PTR      string     `json:"ptr,omitempty"`
	AS       asn.ASInfo `json:"as,omitempty"`
	Country  string     `json:"country,omitempty"` // где стоит сервер (AS.Country - где зарегистрирована сеть); у CDN места нет
	Hosts    []string   `json:"hosts"`
	MX       []string   `json:"mx,omitempty"`
	NS       []string   `json:"ns,omitempty"`
	CDN      bool       `json:"cdn,omitempty"`
	Own      bool       `json:"own,omitempty"`
	Internal bool       `json:"internal,omitempty"`
	Shared   []string   `json:"shared,omitempty"` // чужие сайты, которые и сейчас на адресе
	// SharedSeen: сколько имён видели на адресе за всё время - чужих и своих.
	SharedSeen int `json:"shared_seen,omitempty"`
	// Services: что сканеры видели на своём адресе; nil - не спрашивали или данных нет. Scanned - спросили и ответ был.
	Services *internetdb.Info `json:"services,omitempty"`
	Scanned  bool             `json:"scanned,omitempty"`
	// Open: что ответило на портах, когда проверили сами; Checked - проверили.
	Open    []portscan.Service `json:"open,omitempty"`
	Checked bool               `json:"checked,omitempty"`

	direct bool     // имя домена ведёт сюда само, а не через чужой сервис
	cnames []string // куда по дороге ведут имена
	named  bool     // владельца сети и обратное имя уже спросили
	seen   bool     // порты уже спросили
	met    bool     // соседей уже спросили
	tried  bool     // порты уже проверяли сами
	shaken bool     // сертификат без имени уже спрашивали
}

// Found: итог по именам отчёта. Own - знает хотя бы один свой источник, Added - только сторонние сервисы,
// Silent - сторонние, которые не ответили.
type Found struct {
	Own    int      `json:"own"`
	Added  int      `json:"added"`
	Silent []string `json:"silent,omitempty"`
}

// Server: почтовый сервер или сервер имён домена.
type Server struct {
	Name string   `json:"name"`
	Pref int      `json:"pref,omitempty"`
	IPs  []string `json:"ips,omitempty"`
}

// Report: всё, что собрано по домену.
type Report struct {
	Domain   string    `json:"domain"`
	Hosts    []Host    `json:"hosts"`
	Addrs    []Addr    `json:"addrs"`
	MX       []Server  `json:"mx,omitempty"`
	NS       []Server  `json:"ns,omitempty"`
	TXT      []string  `json:"txt,omitempty"`
	Related  []string  `json:"related,omitempty"`
	Trackers []Tracker `json:"trackers,omitempty"`
	Errors   []string  `json:"errors,omitempty"`
	// Sources: что дал каждый источник имён - свои первыми.
	Sources []SourceState `json:"sources,omitempty"`
	// InsideOnly: сайты, которые настроены на своих серверах, а записи в DNS у них нет.
	InsideOnly []string `json:"inside_only,omitempty"`
	// Found: сколько имён нашли сами и сколько добавили сторонние сервисы.
	Found Found    `json:"found"`
	Notes []string `json:"notes,omitempty"` // что осталось за пределами сбора
	// CRTCached: не пусто, если список из логов сертификатов взят из кэша.
	CRTCached time.Time `json:"crt_cached,omitzero"`

	portsFailed bool
}

// PortsKnown: источник портов ответил про все адреса, которые спрашивали.
func (r *Report) PortsKnown() bool { return !r.portsFailed }
