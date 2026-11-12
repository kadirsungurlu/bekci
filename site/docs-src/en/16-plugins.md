---
id: plugins
title: Plugins (custom checks)
nav: Plugins
description: Add your own checks to the server agent: small signed sh scripts, local pinning, Nagios and JSON output, rules and troubleshooting.
section: Usage
order: 16
slug: plugins
---

A plugin is a small signed `sh` script that the server agent runs at a fixed interval. It brings information Bekci doesn't collect on its own into the panel, such as RAID state, disk health or a metric from your own application. The script returns a state (healthy, warning, critical, unknown), a short message and optional numeric metrics; the panel shows them on the server's **Checks** tab, charts them and turns them into incidents through rules. Plugins are free.

## Security model {#security}

Because a plugin runs code on your server, Bekci requires three locks:

- **Signature:** The manifest is signed with your Ed25519 key. The agent only accepts manifests that verify against public keys you added to it.
- **Local pin:** The body of every plugin is pinned separately on the server, as root (`uptime betik eklenti izin <name> <sha256>`). The agent only runs a body whose sha256 is pinned. Even if the panel is compromised, or the signing key leaks, new content that isn't pinned on the server won't run. All the panel can do is turn a pinned plugin on or off and pick an interval within the manifest's limits.
- **Sandbox:** Each run happens in its own transient systemd unit: user `nobody` by default, no network (`PrivateNetwork`), read-only file system (`ProtectSystem=strict`), private `/tmp`, 128 MB memory, 20% CPU, 32 processes and a timeout. The agent's environment file and `/etc/bekci` aren't visible from inside.

The agent checks the signature, the pin and the validity period again before every run; when you remove the pin or the manifest expires, the next run doesn't happen.

> [!WARNING] Plugin output is visible in the panel
> The message, the metrics and the first 1 KiB of error output are sent to the panel and can be read by everyone who can see the server. Don't write or sign a plugin that prints passwords, tokens or other secrets.

## Requirements {#requirements}

- The agent must run on a Linux server installed with the **Direct (systemd)** method, with systemd 254 or newer (Debian 13, Ubuntu 24.04, RHEL 10 and later).
- Plugins don't run in an agent installed with Docker (`platform` error). There are no plugins on Windows.
- Setting `PLUGINS=0` in the agent's environment turns plugins off completely.
- At most 20 checks per server.

## Step by step {#steps}

### 1. Create a signing key {#key}

On your own machine, once (no root needed). The private key goes to the file, the public key is printed:

```sh
uptime betik eklenti anahtar-uret ~/bekci-plugin.key
```

Keep the private key secret and don't put it on the server running the agent; only the public key goes there.

### 2. Write the body and the manifest {#manifest}

The body (`govde.sh`) is a POSIX `sh` script of at most 64 KiB. The manifest (`manifest.json`) is strict JSON: every field is required, unknown or duplicate fields are rejected.

```json
{
  "schema": 1,
  "id": "mdadm",
  "version": 1,
  "sha256": "",
  "interpreter": "sh",
  "mode": "periodic",
  "class": "metric",
  "run_as": "nobody",
  "platforms": ["linux"],
  "interval_min": 60,
  "interval_max": 3600,
  "timeout_sec": 10,
  "output": "nagios",
  "network": false,
  "not_before": 0,
  "not_after": 0
}
```

| Field | Value |
|---|---|
| `schema` | `1` |
| `id` | Plugin name: lowercase letters, digits and `-`, up to 64 characters. It becomes the check name in the panel and is used by the `izin` command. |
| `version` | An integer starting at 1; increase it for every new body. |
| `sha256` | Leave empty (`""`); the signing tool fills it in from the body. |
| `interpreter` | `sh` |
| `mode` | `periodic` |
| `class` | `metric` |
| `run_as` | `nobody` (recommended) or `root` (only when really needed, e.g. SMART). |
| `platforms` | `["linux"]` |
| `interval_min`, `interval_max` | Limits of the interval you can pick in the panel, in seconds (60-86400). |
| `timeout_sec` | Maximum duration of one run, in seconds (1-60). |
| `output` | `nagios` or `json` (see [output formats](#output)). |
| `network` | `false` recommended; `true` allows network access. |
| `not_before`, `not_after` | Validity (Unix seconds). If both are `0`, the tool sets 90 days from signing; at most 180 days. |

### 3. Sign {#sign}

```sh
uptime betik eklenti imzala ~/bekci-plugin.key manifest.json govde.sh > mdadm.envelope.json
```

The tool writes the body's sha256 into the manifest, validates the manifest and prints the signed envelope. The body sha256 and the `izin` command to run on the agent go to standard error.

### 4. Key and pin on the agent {#pin}

On the server running the agent, as root. The public key is added once; a pin is needed for every plugin and every version:

```sh
sudo uptime betik eklenti anahtar <public key base64>
sudo uptime betik eklenti izin mdadm <body sha256>
```

Settings are written to `/etc/bekci/eklenti.json` (root, 0600).

### 5. Upload in the panel {#panel}

**Servers** > server > **Checks** tab > **Add plugin**. Pick the signed manifest (envelope, `.json`) and the plugin body (`.sh`), set the run interval within the manifest limits, tick **On** and save. The check name comes from the manifest's `id`. The panel passes the envelope and the body to the agent, which schedules the plugin on its next poll.

For each check the tab shows the state, the message, the time and duration of the last run and small charts of the metrics; clicking a metric opens a 24 hour, 7 day or 30 day chart. If the agent rejects a plugin, the reason appears on the tab (see [troubleshooting](#troubleshooting)). Metric history needs PostgreSQL; with SQLite only the latest values are shown.

## Rules {#rules}

Rules are set in the check's edit dialog on the same tab. Incidents go to the server's notification channels and close on their own when things recover.

- **Status rule:** An incident opens when one of the selected states (critical, warning, unknown) lasts the given number of minutes; it closes when the check is healthy again.
- **Metric threshold rule:** Warning and critical thresholds for a metric. By default it fires when the value goes above the threshold; **Lower is worse** reverses the direction (e.g. free space, number of spare disks).

When a check returns unknown 3 runs in a row, it is written to the timeline on the server's **Events** tab; that alone doesn't open an incident.

## Output formats {#output}

### Nagios {#nagios}

The exit code gives the state:

| Code | State |
|---|---|
| 0 | healthy |
| 1 | warning |
| 2 | critical |
| 3 | unknown |
| other, timeout, signal | unknown |

The first line of output is `MESSAGE | perfdata`; on later lines, anything after `|` counts as extra perfdata. Perfdata items are separated by spaces:

```text
RAID OK: 2 arrays, 0 degraded | degraded=0;;0;0 md0.active=2 md0.total=2 'response time'=12ms;200;500;0
```

The syntax is `'label'=value[unit];[warn];[crit];[min];[max]`. Units are `s ms us % B KB MB GB TB c`; durations are converted to seconds and sizes to bytes (base 1024), unknown units are kept as they are. A `U` value is dropped. Threshold ranges:

| Written | Alert when |
|---|---|
| `10` | value below 0 or above 10 |
| `10:` | value below 10 |
| `~:10` | value above 10 |
| `10:20` | value outside 10-20 |
| `@10:20` | value inside 10-20 |

Characters not allowed in a label become `_`.

### JSON {#json}

The output is a single JSON object; the exit code isn't used (a timeout still counts as unknown):

```json
{
  "status": "warning",
  "message": "sda reallocated sectors 8",
  "metrics": {
    "sda.temp_c": {"value": 34, "unit": "C"},
    "sda.reallocated": {"value": 8, "unit": "", "warn": "0", "crit": "50"},
    "sda.pending": 0
  }
}
```

`status` is one of `ok`, `warning`, `critical`, `unknown`; a missing or unrecognized value counts as unknown. A metric value can be an object or a plain number; `warn` and `crit` use the range syntax above.

A flat form is also accepted (when there is no `metrics`): numeric top-level fields are metrics, the `units` object gives units, `status` `1` means healthy and `0` critical (healthy when missing), and the message is in `msg`:

```json
{"status": 1, "msg": "queue normal", "queue": 12, "latency": 0.4, "units": {"latency": "s"}}
```

Invalid JSON results in unknown.

### Limits {#limits}

- Metric names must match `^[a-zA-Z0-9_.-]{1,64}$`; in JSON, names that don't are dropped.
- At most 50 metrics per plugin; extra metrics, duplicate names, NaN and infinite values are dropped and the dropped count appears on the tab.
- The message is at most 512 characters.
- Output is at most 64 KiB: Nagios output is cut, JSON is rejected. The first 1 KiB of error output (stderr) is kept.

## Updating and removing {#update}

When you change the body:

1. Increase `version` in the manifest.
2. Sign again (`imzala`) and note the new body sha256.
3. On the agent, run `sudo uptime betik eklenti izin <name> <sha256>` with the new sha256 (it replaces the old pin).
4. In the panel, upload the new envelope and body from the check's edit dialog.

If the pin on the agent and the version in the panel don't match, the plugin doesn't run and shows `pin_mismatch`; it recovers once both are on the same version. Re-sign and upload before the manifest expires.

To stop a plugin on the server completely, remove its pin as root; the next run doesn't happen:

```sh
sudo uptime betik eklenti kapat mdadm
```

To only switch the check off in the panel, untick **On** in the edit dialog.

## Troubleshooting {#troubleshooting}

When the agent doesn't run a plugin, one of these codes appears on the tab:

| Code | Meaning | What to do |
|---|---|---|
| `signature` | The signature doesn't verify with the agent's keys. | Add the public key with `uptime betik eklenti anahtar` or sign with the right key. |
| `manifest` | The manifest is invalid (missing or unknown field, value out of range). | Fix the manifest and sign again. |
| `name` | The manifest id doesn't match the check name. | Upload the right envelope. |
| `body` | The body doesn't match the sha256 in the manifest. | Upload the body you signed. |
| `not_pinned` | The agent has no pin for this plugin. | `sudo uptime betik eklenti izin <name> <sha256>` |
| `pin_mismatch` | The pin on the agent belongs to another version. | Pin again with the new sha256. |
| `expired` | The manifest has expired. | Sign again and upload. |
| `not_yet_valid` | `not_before` is in the future. | Check the server clock and the manifest. |
| `interval` | The interval is outside the manifest limits. | Pick an interval within the limits in the panel. |
| `disabled` | The agent has no plugin configuration, or it's broken. | Add your signing key first with `uptime betik eklenti anahtar`. |
| `platform` | This agent can't run plugins. | Needs Linux + systemd 254 and a direct (systemd) install. |

To see the keys, pins and the state of the log chain on the agent:

```sh
sudo uptime betik eklenti liste
```

Turning plugins on and off, version changes, pins and a daily summary are kept as a chained log in `/var/log/bekci-eklenti/kayit.jsonl`; individual runs aren't logged. To try a plugin by hand, run the body directly (`sh govde.sh; echo $?`); remember that inside the sandbox there is no network and the file system is read-only.

## Examples {#examples}

The [scripts/eklentiler](https://github.com/kadirsungurlu/bekci/tree/main/scripts/eklentiler) folder in the repository has two ready examples:

- `mdadm`: reads software RAID arrays from `/proc/mdstat` (active/total disks, degraded arrays, sync percentage); Nagios output, runs as `nobody`.
- `smart`: disk health, temperature, reallocated and pending sectors, NVMe wear and media errors through `smartctl`; JSON output, needs the `smartmontools` package and `run_as: root`.
