---
id: scripts
title: Script execution
nav: Script execution
description: Run signed maintenance scripts on your servers: setup, the bekci-imza tool, the approval flow, emergency stop and security limits.
section: Maintenance and reference
order: 16
slug: script-execution
---

Script execution starts small maintenance jobs you signed in advance (log cleanup, service restart, certificate renewal) on selected servers from the panel. The feature is **off by default**: it can only be turned on as root on the server, and the panel cannot turn it on by itself.

The panel is not a shell. It can only run a script you signed and that server allows, with its declared parameters. The final decision belongs to the agent on the server, not the panel.

## Requirements

- A PostgreSQL database. With SQLite the Scripts screen is unavailable (see [Database](/en/docs/database/)).
- An admin account with two-factor authentication. Everything except reading asks for a verification code.
- A Linux agent running under systemd. API keys cannot use the script endpoints; actions come from a browser session.
- A machine outside the panel for signing. Keys never go on the panel, the servers or CI.

## Setup

### 1. Signing tool and keys

`bekci-imza` works offline and never connects to the network:

```sh
go build -o bekci-imza ./cmd/bekci-imza
./bekci-imza anahtar-uret --yuva yayin --cikti yayin.anahtar
./bekci-imza anahtar-uret --yuva onay --cikti onay.anahtar
```

The passphrase is read from stdin. The two keys do different jobs: the **publish key** (`yayin`) signs scripts, the **approval key** (`onay`) signs single-use tickets. Back the files up offline. Add the printed public keys under Scripts > Trust and control > Trust keys.

### 2. Enabling on a server

As root on each server:

```sh
uptime betik ac --yayin-anahtar <public key> --onay-anahtar <public key> \
  --grup temizlik --izin journal-temizle=<body digest>
```

The command writes `/etc/bekci/betik.json` and prints the server's `host_key`. Compare it with the value the panel shows for that server, then approve the key under Trust and control > Servers.

| Command | Purpose |
|---|---|
| `uptime betik izin <id=digest>` or `--sil <id>` | Changes the server's allow list |
| `uptime betik anahtar --yayin-ekle`, `--onay-sil` ... | Changes the trusted keys |
| `uptime betik iptal <envelope file>` | Applies a signed revocation without the panel |
| `uptime betik kapat` | Turns the feature off at once and stops a running script |

An `--izin` value can be `id`, `id=digest` or `id=digest:min_approval`. Entries without a digest (unpinned) always need a signed ticket. There are no wildcards.

### 3. Signing and uploading a script

```sh
./bekci-imza imzala --anahtar yayin.anahtar \
  --bildirim scripts/betikler/journal-temizle/manifest.json \
  --govde scripts/betikler/journal-temizle/govde.sh --cikti imzali/
```

Read the body first: a signature means "I read this and it may run". The output is `<id>-<version>.betik.json`; add it with **Upload script** on the Scripts screen. A signature is valid for 90 days by default and 180 at most. Re-sign an expired script with a new version.

Starter scripts live in the repository under `scripts/betikler/`:

| Script | Group | Approval | Limit |
|---|---|---|---|
| `journal-temizle` | `temizlik` | none | Once per 6 hours |
| `paket-onbellek` | `temizlik` | none | Once per 6 hours |
| `docker-derleme-temizle` | `docker` | panel | Once per 6 hours |
| `docker-imaj-temizle` | `docker` | panel | Once a day |
| `servis-yeniden` | `sistem` | signed | Once per 10 minutes |
| `servis-yeniden-web` | `web` | signed | Once per 10 minutes |
| `sertifika-yenile` | `web` | signed | Once a day |

## Approval flow

In the library, **Run** lets you pick a server and parameters and enter a verification code. What follows depends on the script's approval class. The effective class is the highest of the signed class, the server's local `min_approval` and its pin state; the panel cannot lower it.

| Class | What it takes |
|---|---|
| `none` | The requester's verification code is enough |
| `panel` | A second admin approves. With a single admin, a signed ticket is required |
| `signed` | An offline-signed ticket bound to one server and one run |

### Ticket

Save the request JSON from the waiting run's detail page as `istek.json`:

```sh
./bekci-imza bilet --anahtar onay.anahtar --istek istek.json --onaylayan kadir --igneler igneler.json
```

`igneler.json` is a `{"<host_key>": "server name"}` file you maintain yourself; it never comes from the panel. Read the script, version, parameters and server on screen, then type `evet`. Do not dismiss an unknown server warning. Enter the resulting ticket with **Paste ticket** on the same page.

### Result

The detail page follows the state live. The agent's statement is signed with its own key. Output is shown last and rendered as plain text. If an agent refuses a job, the reason appears in readable form.

## Emergency stop

**Emergency stop** on the Trust and control page needs no verification code: waiting jobs are canceled and no new run can start. **Resume** asks for a code. To stop a single server without the panel, use `uptime betik kapat`.

## Revocation and log verification

To withdraw a script, sign a revocation envelope and upload it:

```sh
./bekci-imza iptal --anahtar yayin.anahtar --id journal-temizle --alt-surum 2
```

The panel writes every action to a chained log. The panel's own "consistent" claim is not proof: export the log, check it outside the panel network and put that in a scheduled job:

```sh
./bekci-imza dogrula --kayit bekci-betik-kayit.json --ajan-kayit kayit.log \
  --bekle zincir-basi.json --en-eski-kontrol 25h
```

Exit code 0 means intact, 1 means broken. `--bekle` keeps the previous chain head and catches rollback and forks. The witness log on the server is `/var/log/bekci-betik/kayit.log`.

## Security limits

- **Protected:** a compromised panel cannot write its own script, change a body, start a `signed` job without a signature, run a script the server does not allow, or exceed the limits.
- **Not protected:** a stolen admin session and code can run allowed `none` and `panel` scripts within their limits. That is why `none` scripts must not interrupt services or delete witness logs.
- **Scripts run as root.** This feature is a form of granting root on the server; keep the allow list narrow.
- `sshd`, the agent, `docker` and database services are never added to the restart lists.
- If the publish key leaks, generate a new one and remove the old one on every server with `uptime betik anahtar`. The panel cannot change that list.
