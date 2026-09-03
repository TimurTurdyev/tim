BIN      := tim
PKG      := github.com/TimurTurdyev/tim
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/cmd.version=$(VERSION)
BENCHCFG := $(shell mktemp -t tim-bench)

.PHONY: build install man test vet lint clean bench inittrace benchinit snapshot release-check

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

MANDIR := $(shell go env GOPATH)/share/man/man1

# man ищет страницы рядом с каталогом из PATH: для ~/go/bin это ~/go/share/man.
install: man
	go install -ldflags "$(LDFLAGS)" .
	mkdir -p $(MANDIR)
	rm -f $(MANDIR)/tim.1 $(MANDIR)/tim-*.1
	cp man/*.1 $(MANDIR)/

man:
	rm -rf man
	go run -ldflags "-X $(PKG)/cmd.version=$(VERSION)" ./tools/genman man

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint не найден"; exit 0; }; golangci-lint run ./...

clean:
	rm -rf $(BIN) man

# Замер старта на пустом конфиге, чтобы числа не зависели от личного config.toml.
bench: build
	@printf 'dev = "/tmp"\n' > $(BENCHCFG)
	@if command -v hyperfine >/dev/null; then \
		TIM_CONFIG=$(BENCHCFG) hyperfine -N -w 5 -r 50 './$(BIN) --version' './$(BIN) proj here' './$(BIN) init zsh'; \
	else \
		for c in '--version' 'proj here' 'init zsh'; do \
			s=$$(perl -MTime::HiRes=time -e 'print time'); \
			for i in $$(seq 50); do TIM_CONFIG=$(BENCHCFG) ./$(BIN) $$c >/dev/null 2>&1; done; \
			e=$$(perl -MTime::HiRes=time -e 'print time'); \
			perl -e "printf \"%-12s %.2f ms\n\", '$$c', ($$e-$$s)*1000/50"; \
		done; \
	fi
	@rm -f $(BENCHCFG)

inittrace: build
	@GODEBUG=inittrace=1 ./$(BIN) --version 2>&1 >/dev/null | sort -t@ -k2 | awk '{print $$5, $$6, $$2}' | sort -rn | head -15

benchinit:
	@command -v benchinit >/dev/null || go install mvdan.cc/benchinit@latest
	benchinit ./cmd

# Локальная сборка релиза без публикации: dist/ с universal-бинарником и checksums.txt.
snapshot:
	goreleaser release --snapshot --clean --skip=publish

release-check:
	goreleaser check
