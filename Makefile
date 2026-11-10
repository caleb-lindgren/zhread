GO ?= go
PREFIX ?= /usr/local

LIB := $(wildcard zhdict/*.go internal/*/*.go) go.mod go.sum

.PHONY: all install clean test apkg dict cards-gif cards-set

all: bin/gozhread

bin/gozhread: $(wildcard cmd/zhread/*.go) $(LIB)
	$(GO) build -o $@ ./cmd/zhread

# Downloads the dictionaries and builds dict/'s tables. ARGS=--csld and
# ARGS=--wikt add the optional extras; see scripts/fetch_dict.sh.
dict: .venv/.installed
	PYTHON=.venv/bin/python scripts/fetch_dict.sh $(ARGS)

# anki/zhread.apkg, the note type for Anki, rebuilt after a template change.
# It is committed, so this is needed only when anki/ changes.
apkg: .venv/.installed
	.venv/bin/python scripts/make_apkg.py

# zhread-cards.gif, the cards a demo made as Anki shows them: CARDS names
# the card file scripts/zhread_demo.sh saved (see the README's demo
# section). The first run installs Playwright and its Chromium into .venv.
cards-gif: cards-set .venv/.demo
	.venv/bin/python scripts/cards_demo.py $(CARDS)

# Checked before anything is installed.
cards-set:
	$(if $(CARDS),,$(error set CARDS to the card file the demo saved))

.venv/.demo: requirements-demo.txt .venv/.installed
	.venv/bin/pip install -q -r requirements-demo.txt
	.venv/bin/playwright install chromium
	touch $@

.venv/.installed: requirements.txt
	python3 -m venv .venv
	.venv/bin/pip install -q -r requirements.txt
	touch $@

test: all
	$(GO) test ./...

# Installs the binary and the st-window wrapper. The binary finds the
# dictionaries by walking up from its own location, which an installed copy
# cannot do, so it is pointed at this checkout's dict/ instead.
install: all
	install -d $(PREFIX)/bin
	install -m 755 bin/gozhread $(PREFIX)/bin/gozhread
	install -m 755 zhread $(PREFIX)/bin/zhread
	@echo
	@echo "Set ZHREAD_DICT=$(CURDIR)/dict in your shell profile,"
	@echo "or run zhread from inside $(CURDIR)."

clean:
	rm -rf bin
