package procs

import (
	"path/filepath"
	"strings"
)

// services: службы, которые видно по имени файла.
var services = map[string]string{
	"postgres": "PostgreSQL", "postmaster": "PostgreSQL", "mysqld": "MySQL", "mariadbd": "MariaDB",
	"redis-server": "Redis", "valkey-server": "Valkey", "memcached": "Memcached", "mongod": "MongoDB",
	"meilisearch": "Meilisearch", "searchd": "Manticore Search", "typesense-server": "Typesense",
	"clickhouse": "ClickHouse", "clickhouse-server": "ClickHouse", "minio": "MinIO", "mailpit": "Mailpit",
	"MailHog": "MailHog", "ollama": "Ollama", "nginx": "nginx", "caddy": "Caddy", "httpd": "Apache",
	"adb": "Android Debug Bridge", "etcd": "etcd", "consul": "Consul",
}

// devTools: dev-серверы, которые видно по слову в команде: node .../node_modules/.bin/vite.
var devTools = map[string]string{
	"vite": "Vite", "vitepress": "VitePress", "next": "Next.js", "nuxt": "Nuxt", "nuxi": "Nuxt",
	"astro": "Astro", "webpack-dev-server": "webpack", "uvicorn": "uvicorn", "gunicorn": "gunicorn",
	"puma": "Puma", "http.server": "http.server",
}

// Service: что за служба или dev-сервер, по имени файла и команде; пусто - не узнали.
func Service(comm, args string) string {
	base := filepath.Base(comm)
	if name, ok := services[base]; ok {
		return name
	}
	if strings.HasPrefix(base, "php-fpm") {
		return "PHP-FPM"
	}
	// php -S, которым artisan serve поднимает сайт: по -S это просто PHP, по server.php - Laravel.
	switch {
	case strings.Contains(args, "Illuminate/Foundation/Resources/server.php"):
		return "Laravel"
	case strings.HasPrefix(args, "next-server"):
		return "Next.js"
	}
	tokens := strings.Fields(args)
	for i, t := range tokens {
		word := filepath.Base(t)
		next := ""
		if i+1 < len(tokens) {
			next = tokens[i+1]
		}
		if name, ok := devTools[word]; ok {
			return name
		}
		switch {
		case word == "artisan" && next == "serve":
			return "Laravel"
		case word == "manage.py" && next == "runserver":
			return "Django"
		case word == "webpack" && next == "serve":
			return "webpack"
		case word == "hugo" && next == "server":
			return "Hugo"
		case word == "rails" && (next == "server" || next == "s"):
			return "Rails"
		case word == "flask" && next == "run":
			return "Flask"
		case t == "-S" && strings.HasPrefix(base, "php"):
			return "PHP"
		case strings.HasPrefix(t, "org.elasticsearch."):
			return "Elasticsearch"
		case strings.HasPrefix(t, "org.opensearch."):
			return "OpenSearch"
		case base == "beam.smp" && strings.Contains(t, "rabbit"):
			return "RabbitMQ"
		}
	}
	return ""
}
