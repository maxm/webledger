# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Webledger is a Go web app that serves as a UI in front of [ledger-cli](https://www.ledger-cli.org/). Each "ledger" is a Git repo containing a plain-text ledger file; the server clones, reads, edits, and pushes changes to those repos on behalf of users authenticated via Google OAuth.

## Commands

```bash
go build                    # produces ./webledger
./webledger                 # runs server on :8082
go test ./...               # run all tests (requires sample_bank_statements/)
go test -run TestParseVisaItauMovimientos   # run a single test
deploy/deploy.sh            # cross-compiles for linux/386 and rsyncs to `server` host (systemd unit at deploy/webledger.service)
go run ./cmd/debug_pdf <file.pdf>   # dump raw text from a PDF for parser debugging
```

`ledger` (the CLI binary) must be on `$PATH` — the server shells out to it for every query (see `LedgerExec` in `ledger.go`).

## Required local files (gitignored)

- `tokens.go` — `package main` defining `ClientId` and `ClientSecret` for Google OAuth.
- `ledgers.json` — map of `{ledger_name: {url, path, users[], notify[]}}`. `url` is a git remote, `path` is the ledger file inside that repo, `users` is the email allowlist.
- `repos/` — populated automatically on startup; each ledger is `git clone`d into `repos/<name>/`.

## Local vs production

`initConfig()` in `main.go` switches on `WEBLEDGER_ENV` (or hostname heuristics):
- **local**: `RootPath=""`, OAuth redirect `http://localhost:8082/oauthcallback`.
- **production**: `RootPath="/ledger"`, redirect `https://max.uy/ledger/oauthcallback`. All routes are served behind that prefix in prod.

When adding URLs in templates or redirects, always go through `RootPath` — hardcoding breaks one of the two environments.

## Architecture

Everything is `package main` at the repo root. There are no internal packages.

**Request lifecycle** (`main.go`): `mux.Router` matches `{ledger}` against a regex built from `Ledgers()` keys at startup, so adding a ledger requires a server restart. `handleLogin` wraps every authenticated handler — it validates the OAuth cookie, refreshes the token if it expires within 5 minutes, and enforces per-ledger user authorization via `AuthLedger`.

**Ledger I/O** (`ledger.go`): Reads always `git pull` first (`UpdateLedger`); writes pull, write the file, commit with the user's email as author, and push. After each write, `WriteLedger` greps the diff against each `LedgerNotify.Regex` and sends an SMTP email via localhost:25 if matched. Account names are extracted from the raw file via regex in `LedgerAccounts` — not by invoking ledger.

**Templates** (`templates.go`): Loaded once at startup. Every view in `templates/views/*.tmpl` is composed with all layouts in `templates/layout/*.tmpl`. `RenderTemplate` always executes the `"layout"` block. The `handleWithTemplateAndData` helper auto-populates `ledger`, `ledgers`, `accounts`, `ledgerFile`, `balance`, `email`, `root` for every authenticated render.

**Reconciliation** (`bankstatement.go` + `reconcile.go`): Parses uploaded bank statements and matches them against `ledger reg` output for the same account/currency. Two-pass matching: exact (same date, amount diff < 0.001), then fuzzy (date within 2 days, same amount). Unmatched bank transactions are turned into suggested ledger entries via `GenerateLedgerEntries`, which uses `account_mappings.json` to guess the counterpart account by description substring (case-insensitive, whitespace-normalized) and groups same-day same-counterpart transactions into one entry. Unknown descriptions get `Expenses:Unknown` / `Income:Unknown`.

Supported parsers (`bankstatement.go`):
- `ParseBrouStatement` — BROU `.xls`, account `Assets:Bank:BROU`
- `ParseItauStatement` — Itau `.xls`, account `Assets:Bank:Itau`
- `ParseBankStatementCSV` — generic CSV
- `ParseVisaItauStatement` — Visa Itau credit card PDF (returns multiple statements: pesos + dollars)
- `ParseVisaItauMovimientos` — pasted HTML from the Itau web "Movimientos" view

`DetectBankFromFilename` picks the parser when the user doesn't choose an account explicitly. To add a bank: add a parser, extend `DetectBankFromFilename`, and add an option to `templates/views/reconcile.tmpl`.

**Currency handling**: `$` = Uruguayan pesos, `US$` = US dollars. Reconciliation queries the ledger with a commodity filter so the two currencies reconcile independently. `parseLedgerAmount` assumes ledger CLI's US output format (comma thousands, dot decimal).
