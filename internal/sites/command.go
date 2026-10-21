package sites

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// checkOptions: что проверять и как часто обходить по расписанию.
type checkOptions struct {
	local, net, servers, install, uninstall, notify bool
	hours                                           float64
	at                                              string
}

// domainOptions: сколько собирать о домене и куда выводить.
type domainOptions struct {
	fast, fresh, noNeighbors, json, openMap, all bool
	mapOut                                       string
}

// corsOptions: что собирается сделать фронтенд.
type corsOptions struct {
	origin, method string
	headers        []string
	credentials    bool
}

// watchOptions: как часто опрашивать и ждать ли уведомлений.
type watchOptions struct {
	every, timeout time.Duration
	quiet          bool
}

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{checkCommand(env), certCommand(env), dnsCommand(), siteCommand(), auditCommand(env), domainCommand(env),
		mailCommand(env), httpCommand(), ogCommand(), corsCommand(), watchCommand()}
}

func checkCommand(env *app.Env) *cobra.Command {
	var o checkOptions
	c := &cobra.Command{
		Use:   "check [проект...]",
		Short: "Обход хозяйства: git, сайт, сертификат, почта, срок домена по всем проектам",
		RunE:  func(cmd *cobra.Command, args []string) error { return check(env, args, o) },
	}
	f := c.Flags()
	f.BoolVar(&o.local, "local", false, "только локальные проверки, без сети")
	f.BoolVar(&o.net, "net", false, "только сетевые проверки")
	f.BoolVar(&o.servers, "servers", false, "и серверы проектов по ssh: место, юниты, обновления, sshd - только чтение")
	f.Float64Var(&o.hours, "hours", machine.ForgottenAfter.Hours(), "с какого возраста dev-сервер считается забытым")
	f.BoolVar(&o.install, "install", false, "обходить каждый день и присылать уведомление, если что-то плохо")
	f.BoolVar(&o.uninstall, "uninstall", false, "убрать ежедневный обход")
	f.BoolVar(&o.notify, "notify", false, "уведомление системы, если есть плохие строки")
	f.StringVar(&o.at, "at", "09:30", "во сколько обходить (с --install)")
	return c
}

func certCommand(env *app.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "cert [host[:port]...]",
		Short: "TLS-сертификат: один домен - подробно, несколько или без аргументов - сроки списком",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return printExpiries(env.Cfg, args)
			}
			return printCert(args[0])
		},
	}
}

func dnsCommand() *cobra.Command {
	var server string
	c := &cobra.Command{
		Use:   "dns <host> [--server 1.1.1.1]",
		Short: "Записи A, AAAA, CNAME, MX, NS, TXT, CAA и подписана ли зона (DNSSEC)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return printDNS(args[0], server) },
	}
	c.Flags().StringVar(&server, "server", "", "резолвер, например 1.1.1.1")
	c.AddCommand(&cobra.Command{
		Use:   "flush",
		Short: "Сбросить DNS-кэш системы (sudo)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return flushDNS() },
	}, &cobra.Command{
		Use:   "servers",
		Short: "DNS-серверы основного резолвера",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printDNSServers() },
	})
	return c
}

func siteCommand() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "site <домен|url>",
		Short: "Что за сайт: IP и где он, регистрация домена, сертификат, HTTP, CMS",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return printSite(args[0], asJSON) },
	}
	c.Flags().BoolVar(&asJSON, "json", false, "вывод в JSON")
	return c
}

func auditCommand(env *app.Env) *cobra.Command {
	var o auditOptions
	c := &cobra.Command{
		Use:   "audit [домен|url|проект] [--json] [--md|--html [--out файл]]",
		Short: "Аудит сайта снаружи: открытые файлы, заголовки, куки, переход на https",
		Long: `Смотрит сайт так, как его видит любой бот: главную, переход с http на https, заголовки безопасности,
флаги кук, страницу ошибки и пути, которые не должны открываться (.env, .git, дампы базы, журналы, phpinfo,
Telescope, панели баз). Ещё - версии TLS и цепочку сертификата, версии nginx, PHP и CMS без поддержки
(endoflife.date, кэш на сутки), порты баз наружу, robots и sitemap, мягкую 404, www и без www, разметку
главной (title, description, lang, h1, favicon, смешанное содержимое), битые ссылки с главной, сжатие,
HTTP/2 и кэш статики, скорость первого байта, CAA, DNSSEC и разнесённость NS, поддомены с CNAME на чужой
сервис, где имя никто не занял. Находка - по содержимому
ответа, а не по коду 200: заглушки отдают главную на любой путь. --md и --html пишут отчёт для клиента:
у каждой находки почему это важно и что сделать. Проверяй свои сайты и те, что попросили проверить.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return runAudit(env, app.First(args), o) },
	}
	c.Flags().BoolVar(&o.json, "json", false, "вывод в JSON")
	c.Flags().BoolVar(&o.md, "md", false, "отчёт для клиента в Markdown: audit-<сайт>-<дата>.md")
	c.Flags().BoolVar(&o.html, "html", false, "отчёт для клиента одной HTML-страницей")
	c.Flags().StringVar(&o.out, "out", "", "куда записать отчёт")
	return c
}

func domainCommand(env *app.Env) *cobra.Command {
	var o domainOptions
	c := &cobra.Command{
		Use:   "domain [домен|проект] [--fast] [--map] [--json]",
		Short: "Проверить домен целиком: записи, поддомены, адреса, живость, сертификаты",
		Long: `Собирает по домену всё, что отдают публичные источники: регистрацию, записи DNS, поддомены из логов
сертификатов и перебором частых имён, адреса с провайдером и страной, живость каждого имени, сертификаты
и признаки связанных сайтов. Домен уходит в crt.sh, whois.cymru.com и (для соседей по IP) api.hackertarget.com.
Только публичные записи и обычный запрос главной страницы.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return domain(env, app.First(args), o) },
	}
	f := c.Flags()
	f.BoolVar(&o.fast, "fast", false, "без логов сертификатов, только перебор частых имён")
	f.BoolVar(&o.fresh, "fresh", false, "обновить логи сертификатов, не брать из кэша")
	f.BoolVar(&o.noNeighbors, "no-neighbors", false, "не спрашивать соседей по IP")
	f.BoolVar(&o.json, "json", false, "вывод в JSON")
	f.BoolVar(&o.openMap, "map", false, "собрать карту в HTML и открыть")
	f.StringVar(&o.mapOut, "map-out", "", "сохранить карту в файл, не открывая")
	f.BoolVar(&o.all, "all", false, "кратко по всем доменам реестра")
	return c
}

func mailCommand(env *app.Env) *cobra.Command {
	var all bool
	var selectors []string
	c := &cobra.Command{
		Use:   "mail [домен] [--all] [--selector s]",
		Short: "Почта домена: MX, SPF, DKIM, DMARC и что с ними не так",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case all:
				return printMailSummary(env.Cfg)
			case len(args) == 0:
				return ui.Errorf("укажи домен: tim mail example.com (или --all по доменам реестра)")
			}
			return printMail(args[0], selectors)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "сводка по доменам реестра")
	c.Flags().StringSliceVar(&selectors, "selector", nil, "свой DKIM-селектор (можно несколько)")
	return c
}

func httpCommand() *cobra.Command {
	var follow, allHeaders bool
	c := &cobra.Command{
		Use:   "http <url> [-L] [--all-headers]",
		Short: "Сколько отвечает сайт по фазам: DNS, TCP, TLS, первый байт, загрузка",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return printTimings(args[0], follow, allHeaders) },
	}
	c.Flags().BoolVarP(&follow, "location", "L", false, "идти по редиректам")
	c.Flags().BoolVar(&allHeaders, "all-headers", false, "все заголовки ответа")
	return c
}

func ogCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "og <url>",
		Short: "Как ссылка будет выглядеть в Telegram и соцсетях: заголовок, описание, картинка",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return printPreview(args[0]) },
	}
}

func corsCommand() *cobra.Command {
	var o corsOptions
	c := &cobra.Command{
		Use:   "cors <url> --origin https://app.example.com [--method POST] [--header Authorization] [--credentials]",
		Short: "Пустит ли браузер запрос с этого origin, а если нет - чего именно не хватает",
		Long: `Делает то же, что браузер: preflight OPTIONS, если он нужен, и сам запрос с заголовком Origin.
Сравнивает ответы с тем, что собирается сделать фронтенд, и говорит, какой заголовок мешает.
Тело запроса не отправляется: для разбора CORS хватает заголовков.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return checkCORS(args[0], o) },
	}
	c.Flags().StringVar(&o.origin, "origin", "", "откуда запрос, например https://app.example.com")
	c.Flags().StringVar(&o.method, "method", "GET", "метод запроса фронтенда")
	c.Flags().StringSliceVar(&o.headers, "header", nil, "свои заголовки запроса (можно несколько)")
	c.Flags().BoolVar(&o.credentials, "credentials", false, "запрос с куками (credentials: include)")
	return c
}

func watchCommand() *cobra.Command {
	var o watchOptions
	c := &cobra.Command{
		Use:   "watch <url> [--every 10s] [--timeout 10s]",
		Short: "Следить за сайтом и сообщить, когда он упал и когда поднялся",
		Long: `Опрашивает адрес раз в --every. Падение - две неудачи подряд (ошибка соединения или код 5xx),
чтобы одно моргание сети не будило зря. На каждой смене состояния - строка и уведомление системы.
Удобно во время деплоя или переезда. Ctrl-C останавливает и печатает итог.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return watchSite(args[0], o) },
	}
	c.Flags().DurationVar(&o.every, "every", 10*time.Second, "как часто опрашивать")
	c.Flags().DurationVar(&o.timeout, "timeout", 10*time.Second, "сколько ждать ответа")
	c.Flags().BoolVar(&o.quiet, "quiet", false, "без уведомлений системы")
	return c
}
