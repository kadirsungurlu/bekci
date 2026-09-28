---
id: start
title: Getting started
nav: Getting started
description: What Bekci is, what you need, and which install path fits you. Decide in a few minutes and continue with the right guide.
section: Getting started
order: 1
slug: ""
---

Bekci watches your websites, APIs and servers from a single panel and tells you right away when something goes wrong. It runs on your own server as a single Docker image: the interface, the check engine, the database and the server agent binaries all ship in the same image.

These docs are written so that you can install Bekci end to end on your own, even if you are not very familiar with Linux and Docker. Every guide is a list of numbered steps, and each step ends with a **“What you should see”** box that tells you whether you are on track.

## What you need {#requirements}

| | Minimum | Recommended |
|---|---|---|
| CPU | 1 vCPU | 1–2 vCPU |
| Memory (RAM) | 512 MB | 1 GB |
| Disk | 10 GB | 10 GB or more |
| CPU architecture | amd64 (x86_64) or arm64 (aarch64) | |
| Operating system | Any Linux distribution that can run Docker | A current Ubuntu LTS or Debian release |

- **Docker:** Bekci ships as a Docker image. If Docker isn't installed yet, [the first step of the Docker guide](/en/docs/install/docker/#install-docker) shows you how.
- **A domain (optional):** to open the panel at a secure address such as `https://bekci.example.com` you need a domain (or subdomain). You don't need one to try Bekci out.
- **Memory use:** Bekci itself uses about 15 MB of RAM with 10 monitors and 3 servers. Add roughly 200 MB if you choose the image with embedded PostgreSQL.

> [!NOTE]
> The commands in these docs assume you are logged in as **root**. If you connect to the server as another user, put `sudo` in front of the commands (e.g. `sudo docker ps`).

## Which install path fits you? {#which-install}

Find the row closest to your situation and open that guide. If you are unsure: **if you have an empty server and a domain, choose the Caddy install**. It gives you a secure (HTTPS), permanent setup with the least effort.

| Your situation | Guide | Time |
|---|---|---|
| I just want to try it; I have a server with Docker | [Docker (one command)](/en/docs/install/docker/) | 3&nbsp;min |
| One server with Docker; I want the settings in a file | [Docker Compose](/en/docs/install/docker-compose/) | 5&nbsp;min |
| An empty server and a domain; I want HTTPS | [HTTPS install (Caddy)](/en/docs/install/caddy/) — **recommended** | 10&nbsp;min |
| My server runs Coolify | [Coolify](/en/docs/install/coolify/) | 5&nbsp;min |
| My server already runs Nginx, Traefik, Apache or Caddy | [Behind a reverse proxy](/en/docs/install/reverse-proxy/) | 10&nbsp;min |

> [!WARNING] Are ports 80 and 443 taken?
> If Coolify or another web server (Nginx, Apache…) already runs on your server, ports 80 and 443 are in use. In that case **don't** use the Caddy install; follow the [Coolify](/en/docs/install/coolify/) or [reverse proxy](/en/docs/install/reverse-proxy/) guide instead.

<div class="doc-cards">
<a class="doc-card" href="/en/docs/install/docker/"><b>Docker (one command)</b>Run one command and open it in your browser. The quickest way to try Bekci.</a>
<a class="doc-card" href="/en/docs/install/docker-compose/"><b>Docker Compose</b>Settings live in one file; easier updates and backups. SQLite or PostgreSQL.</a>
<a class="doc-card" href="/en/docs/install/caddy/"><b>HTTPS install (Caddy)</b>A complete setup on your own domain with an automatic Let's Encrypt certificate.</a>
<a class="doc-card" href="/en/docs/install/coolify/"><b>Coolify</b>Add it as a Docker image and let Coolify handle the domain and certificate.</a>
<a class="doc-card" href="/en/docs/install/reverse-proxy/"><b>Behind a reverse proxy</b>Put it behind your existing Nginx, Traefik, Apache or Caddy.</a>
<a class="doc-card" href="/en/docs/first-steps/"><b>First steps</b>After installing: your first monitor, notification channel and status page.</a>
</div>

## Which database? {#database}

For most installs there is nothing to decide: Bekci uses **SQLite** by default. It needs no extra service, is the lightest option and stays fast with hundreds of monitors.

| Option | How | When |
|---|---|---|
| **SQLite** (default) | The `kadirsungurlu/bekci:latest` image, nothing else | Most installs |
| **Embedded PostgreSQL** | The `kadirsungurlu/bekci:postgres` image; PostgreSQL 18 runs inside the same container | You want PostgreSQL but don't want to run a separate database |
| **External PostgreSQL** | The `latest` image + the `DATABASE_URL` environment variable | You already run a PostgreSQL server |

> [!IMPORTANT]
> Data can **not** be moved between SQLite and PostgreSQL. Pick one from the start. (You can carry monitors, notification channels and status pages over to a new install with the JSON backup under **Settings → Backup / Restore** in the panel; check history is not included.)

## A few terms {#terms}

Terms you will come across while using Bekci:

| Term | Meaning |
|---|---|
| Monitor | A target checked at regular intervals: a website, an API, a port, a database, a cron job… |
| Incident | A period during which a monitor is down, recorded with its start, end and cause. |
| Status page | A public page showing your customers the state of your services. |
| Server agent | A small program installed on a server you want to watch; it sends CPU, RAM, disk and network metrics. |
| Check location | A copy of Bekci that checks your monitors from another location (e.g. another city). |
| `BASE_URL` | The address the panel is reached at from outside. Links in notifications and agent install commands use it. |

## Help {#help}

If you get stuck, start with [Troubleshooting](/en/docs/troubleshooting/). If something in the docs is missing or wrong, or you found a bug, [report it on GitHub](https://github.com/kadirsungurlu/bekci/issues). Bekci is open source ([AGPL-3.0](https://github.com/kadirsungurlu/bekci/blob/main/LICENSE)); the source code is [on GitHub](https://github.com/kadirsungurlu/bekci).

> [!NOTE] About the interface language
> Bekci's panel is available in English and Turkish. Its server logs are written in Turkish; where these docs quote a log line, an English translation is given next to it.
