---
description: "Working with secrets in the Clavis web app: adding and editing values, history and preview, revealing, comparing and copying between environments, importing .env files, and display settings."
---

# Using the web app

The secrets page shows one row per key and one column per environment.
Click a key name to open its panel: value, details and history.

## Adding a secret

**Add secret** (or press `N`) opens a dialog with three kinds:

- **Secret**: masked in the table; every reveal is audited.
- **Variable**: plain configuration, shown as is.
- **File**: a certificate, key or other multi-line value. **Load file**, or
  drop a file on the value box.

The sparkle button next to the value (and next to **Add secret** in the
header) opens the generator: passwords, tokens, JWT signing keys, key pairs
and more, with the raw value shown and an explanation of how it is made.

**Advanced** holds the folder, an expiry date (you get a reminder before it
lapses), a comment, tags, the provider URL (where to rotate it), runbook notes
and the **change message**. The change message appears next to the version in
the key's **History**.

## Changing a value

In a key's panel:

- **Edit value** loads the current value into the editor, so you can fix a
  typo without retyping a long string. Loading it counts as a reveal: it is
  audited, and production asks for a reason.
- **Replace with a new value…** starts empty.
- **Generate** fills in a generated value. **Cancel, keep current value**
  leaves it as it was.

You can also edit inline in the table: hover a cell and click the pencil.
Changes collect in a bar at the bottom; nothing is written until you review
and save.

## History, preview and restore

Every save creates a version. **History** lists them with their change
message. **Preview** shows an old value before you restore it (audited like a
reveal). **Restore** writes the old value back as a new version.

## Seeing values

- Click a masked cell to reveal it, or **Reveal all** in the toolbar to reveal
  the whole table. Production asks for a reason, which is saved in the audit
  log. **Hide all** masks them again.
- **Wide values** gives long values room and scrolls the table sideways.
- The filter (`/`) searches key names, tags, comments, folders, owner groups
  and variable values. Several words must all match.
- **Group by kind** splits the table into secrets, variables and files.

## Comparing and copying between environments

**Compare** shows every environment side by side and marks keys that differ
or are missing. Hover a value and use its copy button to copy it to another
environment. To move many keys at once, with review and a snapshot, use
**Promote**.

## Importing a .env file

**Import** takes a pasted or uploaded `.env`. Values are compared on the
server, so keys whose value is the same are skipped instead of creating new
versions. Pick the kind and folder for the checked keys at once.

## Display settings

**Your account → Appearance** sets the theme and style, the date format
(31.12.2026, 31/12/2026, 12/31/2026 or 2026-12-31), 24- or 12-hour time, and
the fonts for the interface and for code and values (Geist, Manrope, Inter,
IBM Plex Sans, JetBrains Mono, IBM Plex Mono or your system's). They are saved
to your account and follow you to other browsers. All fonts are bundled with
Clavis and load only when chosen.
