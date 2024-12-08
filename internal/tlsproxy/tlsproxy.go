// Package tlsproxy: HTTPS перед локальным dev-сервером с сертификатом своего CA.
package tlsproxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// Handler проксирует на target и говорит приложению, что снаружи был https.
// Без X-Forwarded-Proto фреймворк строит ссылки с http:// и браузер ругается на смешанное содержимое.
func Handler(target *url.URL) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = r.In.Host
			r.SetXForwarded()
			r.Out.Header.Set("X-Forwarded-Proto", "https")
			if _, port, err := net.SplitHostPort(r.In.Host); err == nil {
				r.Out.Header.Set("X-Forwarded-Port", port)
			} else {
				r.Out.Header.Set("X-Forwarded-Port", "443")
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "tim: dev-сервер не отвечает на "+target.Host+": "+err.Error(), http.StatusBadGateway)
		},
	}
}

// Listen занимает addr сразу, чтобы ошибка "порт занят" пришла до запуска dev-сервера,
// и возвращает функцию, которая обслуживает соединения до падения.
func Listen(addr string, target *url.URL, certFile, keyFile string) (func() error, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{
		Handler:           Handler(target),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
	}
	return func() error { return srv.ServeTLS(l, "", "") }, nil
}
