GO ?= go
PREFIX ?= /usr/local

SRC := $(wildcard cmd/gomultizhdict/*.go) go.mod

.PHONY: all install clean test

all: bin/gomultizhdict

bin/gomultizhdict: $(SRC)
	$(GO) build -o $@ ./cmd/gomultizhdict

test: bin/gomultizhdict
	$(GO) test ./...

# Installs the binary and the pager wrapper. The binary finds the
# dictionaries by walking up from its own location, which an installed copy
# cannot do, so the wrapper is pointed at this checkout's dict/ instead.
install: bin/gomultizhdict
	install -d $(PREFIX)/bin
	install -m 755 bin/gomultizhdict $(PREFIX)/bin/gomultizhdict
	install -m 755 multizhdict $(PREFIX)/bin/multizhdict
	@echo
	@echo "Set MULTIZHDICT_DICT=$(CURDIR)/dict in your shell profile,"
	@echo "or run multizhdict from inside $(CURDIR)."

clean:
	rm -rf bin
