GO ?= go
PREFIX ?= /usr/local

LIB := $(wildcard internal/*/*.go) go.mod go.sum

.PHONY: all install clean test apkg

all: bin/gomultizhdict bin/zhread

bin/gomultizhdict: $(wildcard cmd/gomultizhdict/*.go) $(LIB)
	$(GO) build -o $@ ./cmd/gomultizhdict

bin/zhread: $(wildcard cmd/zhread/*.go) $(LIB)
	$(GO) build -o $@ ./cmd/zhread

# anki/zhread.apkg, the note type for Anki, rebuilt after a template change.
# It is committed, so this is needed only when anki/ changes.
apkg: .venv/.installed
	.venv/bin/python scripts/make_apkg.py

.venv/.installed: requirements.txt
	python3 -m venv .venv
	.venv/bin/pip install -q -r requirements.txt
	touch $@

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
