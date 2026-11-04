import type { Shape } from "../types";
import type tr from "../tr/probes";

export default {
  title: "Check locations",
  intro:
    "Check your monitors from other cities or networks too. A check location is a copy of this app running in {probe} mode on another server; it checks the monitors assigned to it and reports the results here. Assign it in the {locations} section of the monitor form.",
  locations: "Locations",
  newProbe: "New check location",
  empty:
    "No check locations yet. All checks currently run from this server only.",
  state: {
    disabled: "Disabled",
    online: "Online",
    offline: "Offline",
  },
  col: {
    tokenPrefix: "Token prefix",
    status: "Status",
    lastSeen: "Last seen",
    address: "Address",
    monitors: "Monitors",
  },
  neverConnected: "Never connected",
  update: {
    current: "Up to date",
    currentTitle: "The agent runs the same version as the panel ({panel}).",
    outdated: "Update available",
    outdatedAuto:
      "Agent {v}, panel {panel}. Automatic updates are on: the agent switches to the new version within seconds.",
    outdatedManual:
      "Agent {v}, panel {panel}. Automatic updates are off for this agent; use “Update now”.",
    requested: "Update requested",
    updating: "Updating…",
    updatingTitle: "The agent is downloading {panel} and restarting.",
    failed: "Update failed",
    failedTitle: "{note}. You can retry with “Update now”.",
    unsupported: "Old agent",
    unsupportedTitle:
      "Agent {v}, panel {panel}. This agent predates automatic updates: run the install command once more and later versions arrive on their own.",
    unsigned: "Unsigned build",
    unsignedTitle:
      "Agent {v}, panel {panel}. This panel build is not signed (local or test build), so it can’t offer updates to agents.",
    unversioned: "Unversioned build",
    unversionedTitle:
      "Agent {v}, panel {panel}. One of them carries a commit hash instead of a version number (test build); automatic updates only work between released versions.",
    newer: "Newer than panel",
    newerTitle:
      "Agent {v}, panel {panel}. The agent is newer; agents are never downgraded.",
    off: "Updates off",
    offTitle: "The agent has AUTO_UPDATE=0 and does not update itself.",
    readonly: "Manual updates",
    readonlyTitle:
      "The agent’s program folder is not writable (for example it runs from the app image); update the image instead.",
    now: "Update now",
    requestedToast:
      "Update requested; the agent switches to the new version within seconds",
    all: "Update all",
    allTitle: "{n} agents can be updated to the panel version ({panel})",
    allToast: "Update requested for {n} agents",
    auto: "Automatic updates",
    autoInherit: "Global setting ({state})",
    autoInheritPlain: "Global setting",
    autoOn: "On",
    autoOff: "Off",
    autoHelp:
      "When the panel moves to a new version, the agent downloads the signed program and updates itself. Global setting: Settings → General.",
  },
  lockedTo: "Locked to IP {ip}",
  lockPending: "IP lock on; pinned on first connection",
  actionsFor: "Actions for {name}",
  foot: "A check location that misses loss threshold heartbeats in a row counts as offline (3 x 5 s = 15 s by default; 90 s for old agents). Agents update themselves along with the panel (Settings → General). Update agents marked “Old agent” once by hand: run {cmd} on the server, then run the install command again (get the command again with “Regenerate token” in the row menu).",
  menu: {
    enable: "Enable",
    disable: "Disable",
    regenerate: "Regenerate token",
  },
  confirm: {
    regenerateTitle: "Regenerate token",
    regenerateMessage:
      "“{name}” — The old token stops working immediately; restart the check location with the new token.",
    regenerateConfirm: "Regenerate token",
    disableTitle: "Disable",
    disableMessage:
      "“{name}” won't be able to send results and will be excluded from location calculations. You can re-enable it later.",
    disableConfirm: "Disable",
    deleteTitle: "Delete check location",
    deleteMessage:
      "“{name}” — This check location will be removed from all monitors.",
  },
  toast: {
    saved: "Check location saved",
    ipReset: "IP lock reset",
    disabled: "Check location disabled",
    enabled: "Check location enabled",
    deleted: "Check location deleted",
  },
  form: {
    errName: "Give the check location a name (e.g. Frankfurt).",
    errNameRequired: "Name is required.",
    namePlaceholder: "e.g. Frankfurt",
    nameHelp:
      "A short name describing the location; shown on monitor details and in notifications.",
    editTitle: "Edit check location",
    ipLock: "Lock to IP",
    ipLockHelp:
      "The check location can only send results from the IP it first connected from; reset the lock if the server moves.",
    lockedIp: "Locked IP: {ip}",
    resetLock: "Reset lock",
    notifyOffline: "Notify when offline",
    notifyOfflineHelp:
      "When loss threshold heartbeats in a row are missed (15 s by default), a “check location unreachable” notification goes to the selected channels and an incident is opened; when it reconnects, a recovery notification closes it. Restarts shorter than that do not notify.",
    channels: "Notification channels",
    noChannels: "No notification channels yet. {link}",
    addChannel: "Add a channel",
    channelOff: "disabled",
    noChannelSelected:
      "No channel selected: no notification is sent, only the incident is recorded.",
  },
  mesh: {
    title: "Connectivity checks (mesh)",
    intro:
      "Check locations ping each other and the panel regularly, so a broken link can be told apart from a real outage. Each check location can override these values.",
    enabled: "Mesh enabled",
    enabledHelp:
      "When off, check locations do not ping each other; only communication with the panel is watched.",
    pingSec: "Mesh ping interval (s)",
    pingSecHelp: "1-60, default 5",
    lossThreshold: "Loss threshold (consecutive misses)",
    lossThresholdHelp: "1-10, default 3",
    panelSec: "Panel communication interval (s)",
    panelSecHelp: "1-60, default 5",
    offlineAfter:
      "Considered offline after = threshold x panel interval = {loss} x {panel} = {n} s",
    saved: "Mesh settings saved",
    errPing: "Mesh ping interval must be 1-60 s.",
    errLoss: "Loss threshold must be 1-10.",
    errPanel: "Panel communication interval must be 1-60 s.",
    errWindow:
      "Loss threshold x interval must be at least 3 s (for both ping and panel interval).",
    errAddr: "Mesh address must be host:port (or empty / -).",
    probeTitle: "Mesh",
    useGlobal: "Use global value ({v})",
    addr: "Mesh address",
    addrPlaceholder: "empty: seen IP:7480, -: not listening",
    addrHelp:
      "host:port other check locations use to reach this one. Enter - if it is behind NAT.",
    fallback: "Fallback notifier",
    fallbackHelp:
      "If Main is unreachable, this check location may send notifications (feature enabled in a later release).",
    effective: "Effective: ping {ping} s, threshold {loss}, panel {panel} s",
  },
  setup: {
    addedTitle: "“{name}” added",
    newTokenTitle: "New token for “{name}”",
    shownOnce: "This token is shown only once; copy it and store it safely.",
    token: "Probe token",
    command: "Install command",
    commandHelp:
      "On the server that will be the check location, paste the whole command as {root} (or change the first line to {sudo}).",
    commandAfter:
      "The token is kept on the server in {path} (with 600 permissions). To update: {cmd}, then run the command again.",
    serverUrl:
      "Server URL: {url}. The check location must be able to reach this URL from outside; it shows up as {online} in the list within a few seconds.",
    copiedClose: "I've copied it, close",
  },
  uplink: {
    primary: "Primary line",
    via: "Via {name}",
    primaryTip: "The agent is connected over the primary line. If the primary line cannot reach Main, it switches to {name}.",
    viaTip: "The primary line cannot reach Main; the agent is connected via {name} ({ip}). Switched: {at}. Results over this line are not counted.",
    ifaceMissing: "OOB interface not found",
    ifaceMissingTip: "The agent could not find the network interface the OOB IP is bound to. Set the interface name on the agent with PROBE_OOB_IFACE.",
  },
  oob: {
    title: "OOB (second line)",
    help: "If the primary line cannot reach Main, the agent connects from the interface this IP is bound to. The IP lock accepts this address too. Leave empty to disable OOB.",
    ip: "OOB IP address",
    ipPlaceholder: "e.g. 203.0.113.10",
    name: "Line name",
    namePlaceholder: "OOB",
    nameHelp: "Shown in notifications and on the badge; up to 40 characters.",
    errIp: "Enter a valid IPv4 or IPv6 address.",
    errName: "The line name can be up to 40 characters.",
  },
  topo: {
    label: "Check location link diagram",
    healthy: "Two-way reachability OK",
    pair: "{a} - {b}: {state}",
    dir: "{src} > {dst}: {state}",
    loss: "consecutive losses: {n}",
    lastOk: "last success {when}",
    never: "no successful measurement yet",
    state: { up: "Healthy", down: "Broken", oneway: "One-way", unknown: "Not measured" },
    count: { up: "{n} healthy", down: "{n} broken", oneway: "{n} one-way", unknown: "{n} not measured" },
  },
  matrix: {
    title: "Link status",
    intro: "Whether the location in the row can reach the one in the column. Refreshes live once per panel interval.",
    inactive: "Link status needs mesh enabled and at least two check locations sending heartbeats with a current agent.",
    healthy: "All known links are healthy.",
    from: "Source",
    up: "Reaches",
    down: "Cannot reach",
    unknown: "Not measured",
    cell: "{src} > {dst}: {state}",
  },
} satisfies Shape<typeof tr>;
