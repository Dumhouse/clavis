---
description: "Run rails c, irb, psql or a shell with Clavis secrets, what output masking does to the values you print, and when to turn it off."
---

# Consoles and masking

`clavis run` works with interactive programs too: a Rails console, `irb`,
`psql`, `node`, a shell. In a terminal the command runs on a pseudo-terminal,
so you get its normal prompt, colors and line editing.

```sh
clavis run rails c
```

## Masking: with and without

By default, Clavis replaces every secret value in the command's output with
`••••••`. Your code still gets the real value; only what is **printed** is
hidden. That keeps secrets out of logs, terminal recordings and screen shares.

**With masking (the default)**

```text
$ clavis run rails c
backend(dev):001> ENV['SIDEKIQ_WEB_PASSWORD']
=> "••••••"
backend(dev):002> ENV['SIDEKIQ_WEB_PASSWORD'] == params[:password]
=> true
```

The second line shows the value is real: the comparison works.

**Without masking**

```text
$ clavis run --no-mask rails c
backend(dev):001> ENV['SIDEKIQ_WEB_PASSWORD']
=> "correct-horse-battery-staple"
```

`--no-mask` goes **before** the command, because everything from the first
word of the command on belongs to it. Use it only on your own screen: values
print in clear text, so keep it out of screen shares, recordings, CI logs and
anything you paste into a chat or a ticket.

## Showing only some values

`--reveal` unmasks the keys you name and keeps everything else hidden, which
is safer than `--no-mask` when you only need one value:

```text
$ clavis run --reveal SIDEKIQ_WEB_PASSWORD rails c
backend(dev):001> ENV['SIDEKIQ_WEB_PASSWORD']
=> "correct-horse-battery-staple"
backend(dev):002> ENV['DATABASE_URL']
=> "••••••"
```

Repeat the flag or separate keys with commas. It only works when the output
is your own terminal, so a script cannot send a revealed value to a file or a
log. If two keys hold the same value and only one is revealed, that value
stays masked.

When a session hid something, `clavis run` prints one line at the end that
tells you about `--reveal` and `--no-mask`.

## Reading one value instead

You don't need to open a console to look at a single value:

```sh
clavis secrets get SIDEKIQ_WEB_PASSWORD
```

Every read is recorded in the audit log, with or without masking.

## What masking does not do

Masking looks for the exact value and its common encodings (base64, URL
encoding) in the output. It is a convenience, not a security boundary: a
program that deliberately changes a value (reverses it, splits it, hashes it)
can still show it. Treat anything the command prints as something that may
contain secrets, and don't run commands you don't trust.

## On Windows

On Windows 10 version 1809 and later, `clavis run` uses a pseudo console
(ConPTY), so consoles such as `irb` or `psql` draw their prompts too. Run
`clavis doctor`: its **Consoles** line says whether your system supports it.
If a console still looks frozen, use `--no-pty`, or `clavis shell`.

## In scripts and CI

When the output isn't a terminal (a pipe, a file, a CI job), the command's
output is piped through the masker instead of using a pseudo-terminal, and
masking works the same way. Add `--no-pty` to get that behavior in a terminal
too, for example if a program misbehaves on a pseudo-terminal.

See also the [FAQ](../faq) and the [`clavis run` reference](../reference/run).
