BIN      := tim
PKG      := github.com/TimurTurdyev/tim
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/cmd.version=$(VERSION)
BENCHCFG := $(shell mktemp -t tim-bench)

.PHONY: build install test vet lint clean bench inittrace benchinit

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint не найден"; exit 0; }; golangci-lint run ./...

clean:
	rm -f $(BIN)

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
