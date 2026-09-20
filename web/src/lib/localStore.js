// Namespaced localStorage helper for the settings that have no backend.
//
// Keys are scoped by user id so that signing into a second account on the
// same browser does not show the first account's display name or reading
// goal. It is still local to one browser and does not sync across devices.

const PREFIX = 'bookish'

let scope = 'anon'

export function setScope(userId) {
  scope = userId == null ? 'anon' : String(userId)
}

function key(name) {
  return `${PREFIX}.${scope}.${name}`
}

export function load(name, fallback) {
  try {
    const raw = localStorage.getItem(key(name))
    return raw === null ? fallback : JSON.parse(raw)
  } catch {
    // A corrupt or hand-edited value should not break the screen it backs.
    return fallback
  }
}

export function save(name, value) {
  try {
    localStorage.setItem(key(name), JSON.stringify(value))
  } catch {
    /* quota or private-mode failure — the feature degrades, the app does not */
  }
}
