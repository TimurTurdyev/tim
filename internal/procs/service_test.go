package procs

import "testing"

func TestService(t *testing.T) {
	cases := []struct{ comm, args, want string }{
		{"/opt/homebrew/opt/postgresql@18/bin/postgres", "postgres -D /opt/homebrew/var/postgresql@18", "PostgreSQL"},
		{"/opt/homebrew/opt/mariadb/bin/mariadbd", "", "MariaDB"},
		{"/opt/homebrew/opt/manticoresearch/bin/searchd", "searchd --config x.conf", "Manticore Search"},
		{"node", "node /Users/dev/shop/node_modules/.bin/vite --port 5173", "Vite"},
		{"node", "node /Users/dev/docs/node_modules/.bin/vitepress dev", "VitePress"},
		{"node", "next-server (v15.1.0)", "Next.js"},
		{"node", "node /Users/dev/app/node_modules/.bin/next dev", "Next.js"},
		{"/opt/homebrew/Cellar/php/8.5.5_1/bin/php", "php -S 127.0.0.1:8003 /Users/dev/shop/vendor/laravel/framework/src/Illuminate/Foundation/Resources/server.php", "Laravel"},
		{"php", "php -S localhost:8000 -t public", "PHP"},
		{"php", "php artisan serve", "Laravel"},
		{"python3", "python3 manage.py runserver 8000", "Django"},
		{"Python", "/opt/homebrew/bin/python3 -m http.server 8766", "http.server"},
		{"php-fpm8.3", "php-fpm: master process", "PHP-FPM"},
		{"java", "java -Xms1g org.elasticsearch.bootstrap.Elasticsearch", "Elasticsearch"},
		{"node", "node server.js", ""},
	}
	for _, c := range cases {
		if got := Service(c.comm, c.args); got != c.want {
			t.Errorf("%s %q: %q, ждали %q", c.comm, c.args, got, c.want)
		}
	}
	if out, errs := ParseOutputs("p42\nf1\ntREG\nn/tmp/vite.log\nf2\ntCHR\nn/dev/ttys003\n"); out != "/tmp/vite.log" || errs != "" {
		t.Errorf("outputs: %q %q", out, errs)
	}
}
