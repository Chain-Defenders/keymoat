# Getting started

By the end of this page your usual commands run through keymoat, and you know what is still in plain text on your machine.

## Before you start

- **macOS or Linux.** On Windows, work inside WSL and follow the Linux steps.
- **Go 1.23 or later**, to build it.
- **Linux only:** [bubblewrap](https://github.com/containers/bubblewrap). `sudo apt install bubblewrap`, or your distribution's equivalent.

## 1. Build and install

```
git clone https://github.com/Chain-Defenders/keymoat
cd keymoat
go build -o keymoat .
sudo mv keymoat /usr/local/bin/
keymoat version
```

Any folder on your `PATH` works in place of `/usr/local/bin`.

## 2. Put it in front of your tools

```
keymoat install --shell
```

This does two things:

- makes a link for each tool you have installed (`forge`, `cast`, `anchor`, `solana`, `spl-token`, `cargo`, `npm`, `npx`, `pnpm`, `pnpx`, `yarn`, `bun`, `bunx`) in `~/.keymoat/bin`;
- adds one line to the end of `~/.zshrc` (or `~/.bashrc` and `~/.bash_profile`) that puts that folder first on your `PATH`.

Without `--shell`, keymoat prints the line and you add it yourself. It must come **after** the lines that nvm, cargo and foundry add, so put it last.

Open a new terminal.

## 3. Check

```
keymoat doctor
```

```
Sandbox
  ✓ works: a sandboxed command could not read a protected file
Tools
  ✓ forge      goes through keymoat  (the tool itself: /Users/you/.foundry/bin/forge)
  ✓ npm        goes through keymoat  (the tool itself: /Users/you/.nvm/versions/node/v24.4.0/bin/npm)
  ...
Keys in plain text
  ✗ .env line 3: PRIVATE_KEY (private key): `keymoat keys move`
```

A `✗` under **Tools** means that tool still runs directly. The fix is almost always the order of lines in your shell's start-up file: see [Troubleshooting](troubleshooting.md#a-tool-goes-around-keymoat).

## 4. Work as before

Run what you always run:

```
forge test
npm install
anchor build
```

Nothing looks different until something is blocked or a command is about to sign. The first time you run a command in a project whose files name RPC endpoints, keymoat asks once whether that project may connect to them:

```
keymoat: this project's files point at 2 hosts keymoat has not seen before:

  eth-mainnet.g.alchemy.com                (.env)
  mainnet.base.org                         (foundry.toml)

Let this project's commands connect to them? [y/N]
```

Answer `y` if they are yours. [Network](network.md) explains why it asks.

## 5. Get your keys out of plain text

The sandbox protects keys from the tools that go through keymoat. It cannot protect a plain file from a program that does not, such as an editor extension. So finish the job:

```
keymoat scan              # what is in plain text
keymoat keys move         # encrypt the keys in this project's env files
```

[Keys](keys.md) walks through both, including what changes in your deploy scripts.

## What to read next

- [How it works](how-it-works.md), to know what you just installed.
- [Signing](signing.md), before your next deploy.
- [Threat model](threat-model.md), for the limits.

## Removing it

```
keymoat uninstall
```

removes the links and the line in your shell's start-up file. Your settings, the log and the vault stay in `~/.keymoat`. If you moved Solana keypairs into the vault, export them first: `keymoat keys export NAME --to FILE`.
