package fill

import "testing"

const conf = `
user www-data;
http {
  # редирект на https
  server {
    listen 80;
    server_name shop.example.com www.shop.example.com;
    return 301 https://$host$request_uri;
  }
  server {
    listen 443 ssl;
    server_name www.shop.example.com shop.example.com;
    root /var/www/shop/current/public;
    location / { try_files $uri /index.php; }
  }
  server {
    listen 443 ssl;
    server_name blog.example.org;
    root /var/www/blog;
  }
  server {
    listen 80 default_server;
    server_name _;
    root /var/www/html;
  }
}
`

func TestParseNginx(t *testing.T) {
	v := ParseNginx(conf)
	if len(v) != 3 {
		t.Fatalf("блоков %d: %+v", len(v), v)
	}
	if v[0].TLS || v[1].Root != "/var/www/shop/current/public" || !v[1].TLS {
		t.Errorf("%+v", v)
	}
	if len(v[0].Names) != 2 || v[0].Names[0] != "shop.example.com" {
		t.Errorf("имена %v", v[0].Names)
	}
}
