# Balise — run the whole thing.
#
#   make up        API + UI on loopback only (the safe default)
#   make expose    same, but the UI listens on every interface so you can port-forward
#   make down      stop both
#   make status    what is running, and what is listening where
#   make logs      tail both logs
#   make test      the full suite
#   make demo      build a small fictional demo vault, index it, print how to run it
#
# Only ONE port ever needs to be reachable. Vite proxies /api to the Go API over
# loopback (see web/vite.config.ts), so the API does not have to listen on a public
# interface for the UI to work, and browser requests stay same-origin — CORS never
# enters it. `make expose` therefore opens the UI port and leaves the API on 127.0.0.1.
#
# /api requires an owner password (BALISE_PASSWORD / PASSWORD=) before it will bind to
# anything other than loopback — it refuses to start otherwise. `make up` stays
# frictionless because its API stays on loopback by default.
#
# If you genuinely need the API reachable too (hitting /api directly, or MCP over HTTP
# from another machine), override it: `make expose API_ADDR=0.0.0.0:8099 PASSWORD=...`.
# Read the warning that target prints first — the password and every request still
# cross the wire in cleartext unless there is a tunnel or TLS in front.

SHELL := /bin/sh

# Not /tmp. systemd-tmpfiles ships `D /tmp`, which empties it on every boot, so a vault
# kept there is lost at the next reboot -- silently, and only once it already holds work
# worth keeping. A vault is a git repository and the source of truth for everything the
# index derives; Postgres can always be rebuilt from it, but nothing can rebuild it.
VAULT     ?= $(HOME)/vaults/work
# Set to a model name to enable semantic search; empty means lexical-only, no downloads.
EMBED_MODEL ?= $(BALISE_EMBED_MODEL)
DSN       ?= postgresql://balise:balise@localhost:5432/balise
DEFAULTS  ?= defaults
API_ADDR  ?= 127.0.0.1:8099
UI_HOST   ?= 127.0.0.1
UI_PORT   ?= 5173
# Owner password gating /api (see cmd/balise/main.go's requirePasswordForNonLoopback).
# Empty is fine as long as API_ADDR stays loopback; required otherwise.
PASSWORD  ?= $(BALISE_PASSWORD)

# The demo vault: a small, entirely fictional vault (cmd/demo-vault) for README
# screenshots and local exploration, kept out of the live VAULT/DSN above so `make demo`
# can never touch the owner's real vault or its index. Its own Postgres schema
# (demo_vault, created by `make demo` itself if missing) keeps it out of the live index too.
# DEMO_ADDR keeps the default port (8099): web/vite.config.ts's dev-server proxy target is
# fixed at 127.0.0.1:8099 (only the host varies for `make expose`), so the API must stay on
# that port for the browser to actually reach it through the Vite dev server. Run the demo
# with `make down` first if the default `make up` pair is already using that port.
DEMO_VAULT    ?= demo-vault
DEMO_DEFAULTS ?= demo-vault-defaults
# Derived from DSN, never hardcoded: `make demo DSN=...` must point every step at the
# same server. docker-compose.yaml publishes 5433 (not 5432, so it cannot collide with a
# system Postgres), so overriding DSN is the normal path, not an edge case. The
# findstring picks & or ? depending on whether DSN already carries a query string.
DEMO_DSN      ?= $(DSN)$(if $(findstring ?,$(DSN)),&,?)search_path=demo_vault,public
DEMO_ADDR     ?= 127.0.0.1:8099

RUN_DIR   := .run
API_PID   := $(RUN_DIR)/api.pid
UI_PID    := $(RUN_DIR)/ui.pid
API_LOG   := $(RUN_DIR)/api.log
UI_LOG    := $(RUN_DIR)/ui.log

API_PORT  := $(lastword $(subst :, ,$(API_ADDR)))

.PHONY: up expose down status logs test build deps check-db check-password restart help demo
.DEFAULT_GOAL := help

help:
	@echo "make up       API + UI on loopback ($(UI_HOST):$(UI_PORT))"
	@echo "make expose   UI on all interfaces, for port forwarding"
	@echo "make down     stop both"
	@echo "make status   what is running"
	@echo "make logs     tail both logs"
	@echo "make test     full suite (go + vitest + playwright)"
	@echo "make demo     build+index a small fictional demo vault, print how to run it"

build:
	@go build -o bin/balise ./cmd/balise

deps:
	@test -d web/node_modules || (cd web && npm install)

# Postgres holds everything derived; without it the API starts and then fails every
# request, which looks like a UI bug rather than a missing service. Fail here instead.
check-db:
	@d="$(DSN)"; pg_isready -q -d "$${d%%\?*}" 2>/dev/null || { \
		echo "Postgres is not accepting connections at $(DSN)"; \
		echo "Start it, or pass another DSN: make up DSN=postgresql://..."; \
		exit 1; }

# The API (cmd/balise/main.go's requirePasswordForNonLoopback) refuses outright to bind
# anywhere but loopback with no owner password set — by design, so the hole this whole
# feature closes can never reopen just because someone forgot a flag. By the time that
# happens under `up`, though, the process is already backgrounded with nohup, so the
# refusal only surfaces as a 10-second readiness-loop timeout pointing at a log file.
# Catch it here instead, before that loop ever starts, with a message that says what to
# do about it.
check-password:
	@case "$(API_ADDR)" in 127.0.0.1:*|localhost:*) ;; *) \
		if [ -z "$(PASSWORD)" ]; then \
			echo "API_ADDR ($(API_ADDR)) is not loopback, so /api requires an owner password."; \
			echo "Pass one: make expose API_ADDR=$(API_ADDR) PASSWORD=... (or export BALISE_PASSWORD)."; \
			exit 1; \
		fi ;; \
	esac

demo: build check-db
	@go run ./cmd/demo-vault -vault "$(DEMO_VAULT)" -defaults "$(DEMO_DEFAULTS)"
	@psql "$(DSN)" -c "create schema if not exists demo_vault" >/dev/null
	@# Embeds too when BALISE_EMBED_MODEL is set. Without this the demo indexes
	@# lexically only, and semantic search silently has nothing to search -- the
	@# symptom is a demo that answers keyword queries and reports "holds little about
	@# this" for the plain-language ones semantic search exists to handle.
	@BALISE_EMBED_MODEL="$(EMBED_MODEL)" ./bin/balise reindex "$(DEMO_VAULT)" --dsn "$(DEMO_DSN)" --defaults "$(DEMO_DEFAULTS)"
	@echo
	@echo "Demo vault ready: $(DEMO_VAULT) (defaults: $(DEMO_DEFAULTS), schema: demo_vault)"
	@echo "Start it:"
	@echo "  make up VAULT=$(DEMO_VAULT) DEFAULTS=$(DEMO_DEFAULTS) DSN='$(DEMO_DSN)' API_ADDR=$(DEMO_ADDR)$(if $(EMBED_MODEL), EMBED_MODEL=$(EMBED_MODEL))"
	@echo "Then open http://localhost:$(UI_PORT)"

up: build deps check-db
	@mkdir -p $(RUN_DIR)
	@$(MAKE) --no-print-directory down >/dev/null 2>&1 || true
	@echo "starting API on $(API_ADDR)"
	@BALISE_DSN="$(DSN)" BALISE_PASSWORD="$(PASSWORD)" BALISE_EMBED_MODEL="$(EMBED_MODEL)" \
		nohup ./bin/balise serve "$(VAULT)" --addr "$(API_ADDR)" --defaults "$(DEFAULTS)" \
		> $(API_LOG) 2>&1 & echo $$! > $(API_PID)
	@echo "starting UI  on $(UI_HOST):$(UI_PORT)"
	@cd web && BALISE_ALLOWED_HOSTS="$(BALISE_ALLOWED_HOSTS)" \
		nohup sh -c 'echo $$$$ > "$$1"; exec node_modules/.bin/vite --host "$$2" --port "$$3" --strictPort' \
		sh "../$(UI_PID)" "$(UI_HOST)" "$(UI_PORT)" > ../$(UI_LOG) 2>&1 &
	@i=0; until curl -fsS "http://127.0.0.1:$(API_PORT)/api/auth/status" >/dev/null 2>&1; do \
		i=$$((i+1)); [ $$i -gt 40 ] && { echo "API did not come up — see $(API_LOG)"; exit 1; }; \
		sleep 0.25; done
	@i=0; until curl -fsS -o /dev/null "http://127.0.0.1:$(UI_PORT)/" 2>/dev/null; do \
		i=$$((i+1)); [ $$i -gt 40 ] && { echo "UI did not come up — see $(UI_LOG)"; exit 1; }; \
		sleep 0.25; done
	@echo
	@echo "  UI   http://localhost:$(UI_PORT)"
	@echo "  API  http://$(API_ADDR)/api  (MCP at /mcp)"
	@echo "  logs make logs     stop  make down"

# Binds the UI to every interface. The API stays wherever API_ADDR says — loopback by
# default — because Vite proxies to it, so one open port is enough.
#
# allowedHosts: Vite refuses requests whose Host header it does not recognise, which is
# a DNS-rebinding guard. Forwarded traffic arrives with whatever host you used, so the
# check has to be relaxed or every request 403s. Set BALISE_ALLOWED_HOSTS to the exact
# hostnames you will use and keep the guard; "all" switches it off entirely.
expose: check-password
	@echo "The UI will accept connections from any interface on port $(UI_PORT)."
	@echo "/api requires the owner password to do anything (session cookie, one"
	@echo "password, no accounts) — but that password and every request still cross"
	@echo "the wire in cleartext unless there is a tunnel or TLS in front. Put this"
	@echo "behind SSH, a tunnel, or a firewall rule; do not hand it a public address"
	@echo "unguarded just because a password is set."
	@case "$(API_ADDR)" in 127.0.0.1:*|localhost:*) ;; *) \
		echo; echo "API_ADDR is $(API_ADDR) — /api is exposed too, not just the UI."; \
		echo "You only need this to reach /api or /mcp directly; the UI does not."; ;; esac
	@echo
	@$(MAKE) --no-print-directory up UI_HOST=0.0.0.0 \
		BALISE_ALLOWED_HOSTS="$${BALISE_ALLOWED_HOSTS:-all}"
	@addr=$$(hostname -I 2>/dev/null | awk '{print $$1}'); \
	 echo "  bound to 0.0.0.0:$(UI_PORT) — this host answers on $$addr"; \
	 case "$$addr" in \
	   10.*|192.168.*|172.1[6-9].*|172.2[0-9].*|172.3[01].*|127.*) \
	     echo "  that is a private address" ;; \
	   *) echo "  THAT IS A PUBLIC ADDRESS. Unless a firewall is dropping $(UI_PORT),"; \
	      echo "  the vault is now readable from the internet by anyone who finds it." ;; \
	 esac

restart: down up

down:
	@# The UI pid file is written by the shell that then execs vite, so the pid it holds
	@# is the server itself. `npx vite` would have recorded a wrapper instead, and killing
	@# a wrapper leaves the real server holding the port -- which is exactly what happened.
	@for f in $(API_PID) $(UI_PID); do \
		[ -f $$f ] || continue; \
		pid=$$(cat $$f); \
		kill -TERM $$pid 2>/dev/null || true; \
		rm -f $$f; \
	done
	@i=0; while ss -ltn 2>/dev/null | grep -qE ':($(API_PORT)|$(UI_PORT))\b'; do \
		i=$$((i+1)); [ $$i -gt 20 ] && break; sleep 0.25; done
	@echo "stopped"

status:
	@for n in api ui; do \
		f=$(RUN_DIR)/$$n.pid; \
		if [ -f $$f ] && kill -0 $$(cat $$f) 2>/dev/null; then echo "$$n  running (pid $$(cat $$f))"; \
		else echo "$$n  not running"; fi; \
	done
	@echo "listening:"
	@ss -ltnp 2>/dev/null | grep -E ':($(API_PORT)|$(UI_PORT))\b' || echo "  (nothing on $(API_PORT) or $(UI_PORT))"

logs:
	@tail -n 40 -f $(API_LOG) $(UI_LOG)

test: build
	@go test ./... -race -count=1
	@cd web && npx vitest run && npm run build && npx playwright test
