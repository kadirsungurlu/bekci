---
id: firewall
title: Firewalls and Cloudflare
nav: Firewalls and Cloudflare
description: If Cloudflare or another firewall protecting your site blocks Bekci's checks, allow every location at once without adding IP addresses one by one.
section: Usage
order: 12
slug: firewall
---

If your site is behind Cloudflare, a WAF or bot protection, Bekci's checks may hit a "403 Forbidden" or "Just a moment…" page. The monitor then shows **Down** even though your site works. With several check locations, adding each IP address is tedious; allow Bekci's **User-Agent** instead.

## Which User-Agent does Bekci use? {#user-agent}

Website checks (HTTP, keyword, JSON) use this User-Agent by default:

```text
Mozilla/5.0 (compatible; Bekci/<version>; +https://bekci.app/bot)
```

The version number changes with updates, so match "**contains Bekci**" in your rules. The main server and all check locations use the same value: check locations receive it from the main server. (Check locations need to be up to date too; older agents use their own default.)

Change it under **Settings → General → User-Agent for checks**; leave it empty for the default. A `User-Agent: …` line in a monitor's **Headers** field overrides it for that monitor.

## Allowing Bekci in Cloudflare {#cloudflare}

1. In the Cloudflare dashboard, open your site, go to **Security → WAF → Custom rules** and click **Create rule**.
2. Rule name: `Bekci`.
3. **Field:** `User Agent`, **Operator:** `contains`, **Value:** `Bekci`.
4. **Action:** `Skip`. Select *All remaining custom rules*, *Rate limiting rules*, *All managed rules* and (if your plan has it) *Super Bot Fight Mode*.
5. Save the rule and move it to the **top** of the list.

In the expression editor the rule looks like this:

```text
(http.user_agent contains "Bekci")
```

**Bot Fight Mode** on the free plan cannot be skipped by custom rules. If checks are still blocked, you may need to turn Bot Fight Mode off under **Security → Bots**.

## A safer option: a secret header {#secret-header}

Anyone can fake a User-Agent. To tie the rule to a value only Bekci knows:

1. Generate a random value, e.g. `openssl rand -hex 16`.
2. Add it to the monitor's **Headers** field: `X-Bekci-Key: <your-value>`.
3. Write the Cloudflare rule expression like this:

```text
(any(http.request.headers["x-bekci-key"][*] eq "<your-value>"))
```

Headers are hidden in the panel after saving, but they are also sent to check locations; treat the value like a password.

## Other firewalls {#other}

- **Nginx:** use `if ($http_user_agent ~* "Bekci") { ... }` to skip limits such as `limit_req`.
- **IP allowlist:** if you still prefer allowing by IP address, the check locations' addresses are listed under **Settings → Check locations**.
