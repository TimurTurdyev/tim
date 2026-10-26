package recipes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTasks(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"Makefile": `.PHONY: build test
VERSION := $(shell git describe)
OUT ?= bin/tim

# собрать бинарник
build: deps
	go build -o $(OUT) .

test: ## тесты без кэша
	go test -count=1 ./...

%.o: %.c
	cc -c $<
`,
		"justfile": `set shell := ["bash", "-c"]

# поднять базу
db-up:
    docker compose up -d db

[private]
helper:
    echo hidden

_secret:
    echo hidden

lint target="./...":
    golangci-lint run {{target}}
`,
		"Taskfile.yml": `version: '3'
vars:
  NAME: shop
tasks:
  migrate:
    desc: "миграции базы"
    cmds:
      - php artisan migrate
  queue:work:
    cmds: [php artisan queue:work]
`,
		"package.json":   `{"name":"shop","scripts":{"dev":"vite","build":"vite build","test":"vitest"}}`,
		"pnpm-lock.yaml": "",
		"composer.json":  `{"scripts":{"post-install-cmd":["@php artisan key:generate"],"test":"phpunit","cs":["php-cs-fixer fix","phpstan"]}}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := []Task{
		{"db-up", "just", "поднять базу"},
		{"lint", "just", ""},
		{"migrate", "task", "миграции базы"},
		{"queue:work", "task", ""},
		{"build", "make", "собрать бинарник"},
		{"test", "make", "тесты без кэша"},
		{"dev", "pnpm", "vite"},
		{"cs", "composer", "php-cs-fixer fix; phpstan"},
	}
	got := Tasks(dir)
	if len(got) != len(want) {
		t.Fatalf("задач %d, ожидали %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, ожидали %+v", i, got[i], want[i])
		}
	}
}
