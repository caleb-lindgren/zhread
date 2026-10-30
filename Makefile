GO ?= go
PREFIX ?= /usr/local

LIB := $(wildcard internal/*/*.go) go.mod go.sum

.PHONY: all install clean test

all: bin/gomultizhdict bin/zhread

bin/gomultizhdict: $(wildcard cmd/gomultizhdict/*.go) $(LIB)
	$(GO) build -o $@ ./cmd/gomultizhdict

bin/zhread: $(wildcard cmd/zhread/*.go) $(LIB)
	$(GO) build -o $@ ./cmd/zhread

test: all
	$(GO) test ./...

# Installs the binaries and the pager wrapper. The binaries find the
# dictionaries by walking up from their own location, which an installed copy
# cannot do, so the wrapper is pointed at this checkout's dict/ instead.
install: all
	install -d $(PREFIX)/bin
	install -m 755 bin/gomultizhdict $(PREFIX)/bin/gomultizhdict
	install -m 755 bin/zhread $(PREFIX)/bin/zhread
	install -m 755 multizhdict $(PREFIX)/bin/multizhdict
	@echo
	@echo "Set MULTIZHDICT_DICT=$(CURDIR)/dict in your shell profile,"
	@echo "or run multizhdict from inside $(CURDIR)."

clean:
	rm -rf bin
