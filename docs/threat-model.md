# Threat model

This page says what KeyMoat is built to stop, what it only makes harder, and what it does nothing about. If you are deciding whether to rely on it, this is the page to read.

## The attacker

Code you did not write, running on your machine as you, because you ran a normal developer command:

- a dependency's install script (`npm install`, `pnpm add`);
- a build script or procedural macro (`cargo build`, `anchor build`);
- test or script code in a dependency, including Solidity that uses `ffi` or the environment cheatcodes;
- a package you ran once with `npx`.

Its goal is your wallet keys, and after those, anything else worth stealing: SSH keys, cloud credentials, registry tokens, browser sessions.

KeyMoat assumes this code is hostile and clever. It does not assume it can be recognised.

## What KeyMoat stops

For a command that goes through KeyMoat and does not sign:

| The code tries to | Result |
|---|---|
| Read a private key from the project's `.env` | The file is unreadable and the variable is not in the environment. |
| Read `~/.config/solana/id.json`, a Foundry keystore, `~/.ssh` | Unreadable. |
| Read a browser profile, a wallet app's data, the login keychain | Unreadable. |
| Read a key from another project's `.env` | Unreadable on macOS. On Linux, unreadable for projects next to this one. |
| Read a key you once typed, from shell history | Unreadable. |
| Send data to its own server | The connection is refused and named when the command ends. |
| Connect by IP address, or look a name up directly | Refused: the only route out is the proxy. |
| Add a line to `~/.zshrc`, drop a git hook, replace a tool link | Not writable. |
| Open another app, script the terminal, ask the keychain for a password | Blocked by the sandbox. |
| Rename, copy or hard-link a hidden `.env` to read it under another name | Refused. |
| Point a keystore name or path at another folder (`--account ../../.ssh`, or the same in a project file) | Refused: one real key file is opened, never a folder. |
| Set `KEYMOAT_ACTIVE` in a project to make KeyMoat think it is already inside a sandbox | Ignored: KeyMoat checks, it does not take the variable's word. |

## What KeyMoat makes harder, not impossible

**Stealing through an allowed host.** A program that may reach GitHub or the npm registry can send data there, for example to a repository the attacker controls. It still cannot read your keys, so what it can send is limited to what the sandbox lets it read: the project's source and non-secret settings. The allowlist limits the channel; it does not close it.

**Poisoning a cache.** Caches are writable, because builds need them. Code can plant something in a cache that runs on a later build. That later build runs inside the sandbox again, as long as it goes through KeyMoat. A tool that reads the same cache without KeyMoat would run it unprotected.

**Tampering with the project.** The project folder is writable. Code can change your source, your `foundry.toml` or a `package.json` script. KeyMoat guards the obvious follow-ups: git hooks cannot be written, a host that newly appears in a project file is not allowed until you say yes to it, and a project file cannot steer which key file is opened. Review what you commit.

**The repository's git settings.** `.git/config` has to stay writable (installing a dependency as a submodule writes it), and several git settings make git run a program. Code can set one, and git would run it outside the sandbox at your next git command. KeyMoat cannot forbid the write, so it compares the settings before and after each run and tells you when such a setting appeared:

```
keymoat: this command changed the repository's git settings to run a program: fsmonitor = /tmp/x.sh
```

Take that message seriously.

**Local services.** A sandboxed command can connect to anything listening on your own machine (on macOS; on Linux only to what it started itself). A local node with unlocked accounts, a database without a password, or a local proxy that forwards to the internet is reachable. Do not run such services unprotected next to untrusted builds.

## What KeyMoat does not stop

**Code that does not go through KeyMoat.** An editor extension runs inside your editor with everything your editor can reach. A script you run with `node script.js` or `python script.py` directly is not sandboxed either (use `keymoat run -- node script.js`). For these, the only protection is that your keys are not in plain files: see [Keys](keys.md).

**The code that runs during a signing command.** When you deploy, the deploy script and its dependencies run with the project's env files readable, and with the key the command names. KeyMoat shows you the command first and keeps every other secret closed, but it cannot let a script sign without letting that script's code near the key. An encrypted keystore or a hardware wallet protects the key itself: the code can ask for a signature, not copy the key.

**A Solana deploy's network.** `solana program deploy` sends to validators directly, which a proxy cannot carry. So `anchor deploy`, and Anchor tests against a live cluster, run with the network open. Files stay protected.

**An attacker who is already outside the sandbox.** If your machine is already compromised, KeyMoat does not help. It also does not defend against someone with your user account, your disk or your password.

**What you approve.** If you answer `yes` to a deploy on the wrong chain, or allow a host that is not yours, KeyMoat does what you said.

**Bugs in the operating system's sandbox.** KeyMoat relies on the system's own mechanism (see [Platforms](platforms.md)). A flaw there is a flaw here.

## Design choices that follow

- **Deny by default.** On macOS the sandbox starts from "nothing is allowed" and adds what a build needs. Routes out of a sandbox that go through other apps stay closed without having to be listed.
- **Values, not names.** A key is recognised by its shape. A variable called `DEPLOYER` holding a private key is hidden; an API key called `SECRET` is not a wallet key and is left alone.
- **Project files are not trusted.** A host named in `foundry.toml` or `.env` needs your yes once, because a dependency can rewrite those files between two runs.
- **Hosts you type are trusted.** `--rpc-url https://...` on the command line is your own intent.
- **Failing closed.** With no terminal to ask on, a signing command does not run and unknown hosts are not allowed.
- **No daemon, no account, no telemetry.** KeyMoat runs for the length of your command and talks to nothing but the endpoint you are about to use, to ask its chain.

## What to do with this

1. Install KeyMoat so builds, tests and installs cannot reach keys.
2. Move every key out of plain text, so code outside the sandbox cannot use it either.
3. For anything of value, sign with a hardware wallet or a multisig. KeyMoat is the layer under that, not a replacement for it.

## Reporting a problem

If you find a way for sandboxed code to read a protected file or connect to a host that is not allowed, see the [security policy](../SECURITY.md).
