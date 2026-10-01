import type { Shape } from '../types';
import type tr from '../tr/probes';

export default {
  title: 'Check locations',
  intro:
    'Check your monitors from other cities or networks too. A check location is a copy of this app running in {probe} mode on another server; it checks the monitors assigned to it and reports the results here. Assign it in the {locations} section of the monitor form.',
  locations: 'Locations',
  newProbe: 'New check location',
  empty: 'No check locations yet. All checks currently run from this server only.',
  state: {
    disabled: 'Disabled',
    online: 'Online',
    offline: 'Offline',
  },
  col: {
    status: 'Status',
    lastSeen: 'Last seen',
    address: 'Address',
    monitors: 'Monitors',
  },
  neverConnected: 'Never connected',
  outdated: 'Outdated',
  outdatedTitle:
    'The check location runs {v}, the panel {server}. Update the agent for synchronized location checks and the new User-Agent: run the command below, then fetch the install command again.',
  lockedTo: 'Locked to IP {ip}',
  lockPending: 'IP lock on; pinned on first connection',
  actionsFor: 'Actions for {name}',
  foot: 'A check location that sent results in the last 90 seconds counts as online. To update, run on the server: {cmd}, then run the install command again (get the command again with “Regenerate token” in the row menu).',
  menu: {
    enable: 'Enable',
    disable: 'Disable',
    regenerate: 'Regenerate token',
  },
  confirm: {
    regenerateTitle: 'Regenerate token',
    regenerateMessage: '“{name}” — The old token stops working immediately; restart the check location with the new token.',
    regenerateConfirm: 'Regenerate token',
    disableTitle: 'Disable',
    disableMessage:
      "“{name}” won't be able to send results and will be excluded from location calculations. You can re-enable it later.",
    disableConfirm: 'Disable',
    deleteTitle: 'Delete check location',
    deleteMessage: '“{name}” — This check location will be removed from all monitors.',
  },
  toast: {
    saved: 'Check location saved',
    ipReset: 'IP lock reset',
    disabled: 'Check location disabled',
    enabled: 'Check location enabled',
    deleted: 'Check location deleted',
  },
  form: {
    errName: 'Give the check location a name (e.g. Frankfurt).',
    errNameRequired: 'Name is required.',
    namePlaceholder: 'e.g. Frankfurt',
    nameHelp: 'A short name describing the location; shown on monitor details and in notifications.',
    editTitle: 'Edit check location',
    ipLock: 'Lock to IP',
    ipLockHelp:
      'The check location can only send results from the IP it first connected from; reset the lock if the server moves.',
    lockedIp: 'Locked IP: {ip}',
    resetLock: 'Reset lock',
  },
  setup: {
    addedTitle: '“{name}” added',
    newTokenTitle: 'New token for “{name}”',
    shownOnce: 'This token is shown only once; copy it and store it safely.',
    token: 'Probe token',
    command: 'Install command',
    commandHelp:
      'On the server that will be the check location, paste the whole command as {root} (or change the first line to {sudo}).',
    commandAfter:
      'The token is kept on the server in {path} (with 600 permissions). To update: {cmd}, then run the command again.',
    serverUrl:
      'Server URL: {url}. The check location must be able to reach this URL from outside; it shows up as {online} in the list within a few seconds.',
    copiedClose: "I've copied it, close",
  },
} satisfies Shape<typeof tr>;
