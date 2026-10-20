package audit

import (
	"regexp"
	"strings"

	"github.com/TimurTurdyev/tim/internal/level"
)

var (
	envRe      = regexp.MustCompile(`(?m)^\s*(APP_KEY|DB_PASSWORD|DB_HOST|DB_USERNAME|SECRET_KEY|DATABASE_URL|MAIL_PASSWORD)\s*=`)
	laravelRe  = regexp.MustCompile(`(?m)^\[\d{4}-\d{2}-\d{2}[ T][\d:.]+[^\]]*\] \w+\.(ERROR|INFO|WARNING|DEBUG|CRITICAL)`)
	sqlRe      = regexp.MustCompile(`(?i)(^|\n)\s*(-- MySQL dump|-- MariaDB dump|-- PostgreSQL database dump|CREATE TABLE|INSERT INTO)`)
	htpasswdRe = regexp.MustCompile(`(?m)^[\w.@-]+:(\$apr1\$|\$2[aby]\$|\{SHA\}|\$[56]\$)`)
	svnRe      = regexp.MustCompile(`^\s*\d+\s*$`)
)

// probes: пути, которые не должны открываться. Находка - только по содержимому: сайты-заглушки отдают
// главную на любой путь с кодом 200, и код ничего не доказывает.
var probes = []probe{
	{[]string{"/.git/HEAD", "/.git/config"}, level.Bad, "открыт репозиторий git",
		"исходники и вся история, вместе с когда-то закоммиченными паролями, скачиваются целиком",
		"закрыть в nginx: location ~ /\\.git { deny all; }, а лучше не держать .git в корне сайта",
		func(b string) bool {
			return strings.HasPrefix(strings.TrimSpace(b), "ref: refs/") || strings.Contains(b, "[core]")
		}},
	{[]string{"/.env", "/.env.bak", "/.env.backup", "/.env.production", "/.env.local"}, level.Bad, "открыт .env",
		"в нём пароли базы, почты и ключ приложения: с APP_KEY подделываются сессии и шифрованные куки",
		"корень сайта - public/, а не папка проекта; в nginx: location ~ /\\.env { deny all; }; пароли и APP_KEY сменить",
		envRe.MatchString},
	{[]string{"/storage/logs/laravel.log"}, level.Bad, "открыт журнал Laravel",
		"в ошибках бывают запросы к базе, пути, письма и данные пользователей",
		"корень сайта - public/; журналы вне корня", laravelRe.MatchString},
	{[]string{"/phpinfo.php", "/info.php"}, level.Bad, "открыт phpinfo",
		"видны версии, пути, модули и переменные окружения - иногда с паролями", "удалить файл с сервера",
		func(b string) bool { return strings.Contains(b, "phpinfo()") }},
	{[]string{"/backup.sql", "/dump.sql", "/db.sql", "/database.sql"}, level.Bad, "открыт дамп базы",
		"база целиком: пользователи, заказы, хэши паролей", "убрать дамп из корня сайта", sqlRe.MatchString},
	{[]string{"/backup.zip", "/site.zip", "/www.zip"}, level.Bad, "открыт архив сайта",
		"исходники и часто .env внутри", "убрать архив из корня сайта",
		func(b string) bool { return strings.HasPrefix(b, "PK\x03\x04") }},
	{[]string{"/wp-config.php.bak", "/wp-config.php~", "/wp-config.old"}, level.Bad, "открыта копия wp-config",
		"пароль базы WordPress открытым текстом", "удалить копии конфига",
		func(b string) bool { return strings.Contains(b, "DB_PASSWORD") }},
	{[]string{"/.htpasswd"}, level.Bad, "открыт .htpasswd", "хэши паролей подбираются офлайн",
		"закрыть в nginx: location ~ /\\.ht { deny all; }", htpasswdRe.MatchString},
	{[]string{"/telescope/requests"}, level.Bad, "открыт Laravel Telescope",
		"запросы, письма, исключения и данные пользователей видны всем", "TELESCOPE_ENABLED=false на проде или вход только для своих в TelescopeServiceProvider::gate",
		func(b string) bool {
			return strings.Contains(b, "Telescope") && strings.Contains(b, "window.Telescope")
		}},
	{[]string{"/_ignition/health-check"}, level.Bad, "включён APP_DEBUG",
		"Ignition отвечает только в отладке: на ошибке видны код, пути и переменные окружения",
		"APP_DEBUG=false в .env на сервере, затем php artisan config:cache",
		func(b string) bool { return strings.Contains(b, `"can_execute_commands"`) }},
	{[]string{"/_profiler/"}, level.Bad, "открыт профайлер Symfony", "запросы, переменные окружения и конфигурация видны всем",
		"APP_ENV=prod на сервере", func(b string) bool { return strings.Contains(b, "Symfony Profiler") }},
	{[]string{"/.svn/entries"}, level.Bad, "открыт репозиторий svn", "исходники скачиваются по списку файлов",
		"закрыть в nginx: location ~ /\\.svn { deny all; }", svnRe.MatchString},
	{[]string{"/.DS_Store"}, level.Warn, "открыт .DS_Store", "по нему видны имена всех файлов в папке, включая забытые",
		"удалить с сервера, в nginx: location ~ /\\.DS_Store { deny all; }",
		func(b string) bool { return strings.HasPrefix(b, "\x00\x00\x00\x01Bud1") }},
	{[]string{"/server-status"}, level.Warn, "открыт server-status Apache", "видны адреса клиентов и все запросы в работе",
		"Require local в <Location /server-status>", func(b string) bool { return strings.Contains(b, "Apache Server Status") }},
	{[]string{"/adminer.php", "/phpmyadmin/"}, level.Warn, "панель базы открыта всем", "её перебирают боты, а старые версии ломают",
		"убрать с сайта или пустить только со своих адресов", func(b string) bool {
			return (strings.Contains(b, "Adminer") && strings.Contains(b, "<form")) || strings.Contains(b, "phpMyAdmin")
		}},
	{[]string{"/composer.json"}, level.Info, "открыт composer.json", "виден список пакетов и версии - проще подобрать уязвимость",
		"корень сайта - public/", func(b string) bool { return strings.Contains(b, `"require"`) }},
}

// botPaths: что боты ищут помимо путей аудита - по содержимому их не проверить, но в журналах сервера
// это те же пробы: входы в CMS, облачные ключи, старые дыры.
var botPaths = []string{"/wp-login.php", "/xmlrpc.php", "/.aws/credentials", "/actuator", "/cgi-bin/", "eval-stdin.php"}

// debugMarks: признаки отладочной страницы ошибки у разных движков.
var debugMarks = []string{
	"You're seeing this error because you have <code>DEBUG = True</code>",
	"Whoops! There was an error.",
	"Action Controller: Exception caught",
	"sf-dump", "Symfony Exception",
	"window.ignite",
}

// probe: один вид открытого файла и пути, по которым его ищут.
type probe struct {
	paths     []string
	level     level.Level
	text, why string
	fix       string
	match     func(body string) bool
}

// ProbePaths: все пути, которые проверяет аудит.
func ProbePaths() []string {
	var out []string
	for _, p := range probes {
		out = append(out, p.paths...)
	}
	return out
}

// ProbePattern: все пробы одним регулярным выражением (POSIX ERE) для grep по журналу сервера.
func ProbePattern() string {
	paths := append(ProbePaths(), botPaths...)
	parts := make([]string, len(paths))
	for i, p := range paths {
		parts[i] = regexp.QuoteMeta(p)
	}
	return strings.Join(parts, "|")
}

// Exposed: путь открылся и отдал то, что не должен. Не 200 - не находка.
func Exposed(path string, status int, body string) (Finding, bool) {
	if status != 200 {
		return Finding{}, false
	}
	for _, p := range probes {
		for _, pp := range p.paths {
			if pp == path && p.match(body) {
				return Finding{Area: "файлы", Level: p.level, Text: p.text + ": " + path, Why: p.why, Fix: p.fix}, true
			}
		}
	}
	return Finding{}, false
}

// DebugPage: страница ошибки показывает отладку движка - код, пути, переменные.
func DebugPage(body string) (Finding, bool) {
	for _, m := range debugMarks {
		if strings.Contains(body, m) {
			return Finding{Area: "файлы", Level: level.Bad, Text: "страница ошибки в режиме отладки",
				Why: "на ошибке видны код, пути, запросы и переменные окружения",
				Fix: "выключить отладку на проде: APP_DEBUG=false, DEBUG = False, APP_ENV=prod"}, true
		}
	}
	return Finding{}, false
}
