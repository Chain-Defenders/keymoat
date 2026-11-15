# keymoat

Keeps wallet keys out of reach of the code you build, test and install.

A web3 developer runs other people's code all day: a dependency's install script, a build script, a test helper, a Solidity library with `ffi`. Any of it can read the private key in `.env`, the Solana wallet in `~/.config/solana/id.json`, the SSH keys, the browser's wallet extension, and send them away. That is how developers have lost funds to fake packages and poisoned dependencies.

keymoat does not try to spot the theft. It makes it impossible: the tools that run a project's code run in a sandbox where the keys cannot be read and nothing can be sent anywhere but the hosts the work needs.

Made by [Chain Defenders](https://chaindefenders.xyz).

**[Documentation](docs/README.md)** · [Getting started](docs/getting-started.md) · [How it works](docs/how-it-works.md) · [Threat model](docs/threat-model.md) · [Troubleshooting](docs/troubleshooting.md)

## What it does

| | |
|---|---|
| **Sandboxed tools** | `forge`, `anchor`, `cargo`, `npm`, `npx`, `pnpm`, `yarn` and `bun` run where keystores, wallet files, SSH and cloud credentials, browser profiles, password stores and shell history cannot be read. |
| **Hidden keys** | A private key in the project's `.env` is invisible to builds, tests and installs. The rest of the file still works. |
| **Network allowlist** | Sandboxed commands reach package registries, compiler downloads, GitHub, block explorers and the RPC endpoints you approved. Everything else is blocked and named. |
| **A question before signing** | A deploy or a send on a live network is shown first: what, which chain, which key. A local node is never asked about. |
| **Throwaway wallet for tests** | `anchor test` on a local validator runs with a wallet that is worth nothing, never your own. |
| **Keys out of plain text** | `keymoat scan` finds keys in plain text. `keymoat keys move` puts an EVM key into a Foundry keystore and a Solana keypair into an encrypted vault. |
| **Editor extensions** | `keymoat extensions` lists installed web3 extensions worth a second look. |

## Install

Build it (Go 1.23 or later):

```
git clone https://github.com/Chain-Defenders/keymoat && cd keymoat
go build -o keymoat . && sudo mv keymoat /usr/local/bin/
```

Then let it stand in front of your tools:

```
keymoat install --shell     # adds one line to the end of ~/.zshrc or ~/.bashrc
```

Open a new terminal and check:

```
keymoat doctor
```

From here on you type `forge test`, `npm install`, `anchor build` as before. Nothing else changes.

macOS and Linux. On Windows, use it inside WSL. Linux needs [bubblewrap](https://github.com/containers/bubblewrap) (`apt install bubblewrap`).

## Day to day

Most commands look the same as before. You notice keymoat in three places.

**A blocked connection** is named when the command ends:

```
keymoat: blocked 2 connection attempts to telemetry.example.com
keymoat: if npm should reach a host: keymoat allow HOST
```

**A command that signs** on a live network waits for you:

```
keymoat: this command signs with a real key.

  What       Run a script that sends transactions (forge script --broadcast)
  Network    Base (chain 8453) through mainnet.base.org
  Signs with encrypted keystore "deployer"
  Target     script/Deploy.s.sol

Type "yes" to continue:
```

The chain is asked of the endpoint itself, so a mislabelled RPC URL shows. Test networks take a `y`. Declining ends the command with an error code.

**A command that needed a key and did not get one** says so:

```
keymoat: PRIVATE_KEY was hidden from this command, like every key. If the command has to sign, run it with KEYMOAT_KEYS=1.
```

### When a command gets keys

| Command | Keys |
|---|---|
| Builds, tests, installs, anything else | None. |
| `forge script --broadcast`, `forge create --broadcast` | The project's env files, and the keystore named with `--account` or `--keystore`. Asked first. |
| `cast send`, `solana transfer`, `solana program deploy`, `spl-token transfer`, ... | Run as they are (they run no project code). Asked first. |
| `anchor deploy`, `upgrade`, `migrate`, `idl init`, tests on a live cluster | The wallet from `Anchor.toml` or `--provider.wallet`. Asked first. |
| `npm run deploy` and the like, when the script deploys (`hardhat ... --network`, `--broadcast`) | The project's env files. Asked first. |
| `npm publish`, `cargo publish`, `login` | The registry token. |
| Anything, with `KEYMOAT_KEYS=1` in front | The project's env files. |

One command without keymoat: `KEYMOAT=off forge ...`. Any other command inside the sandbox: `keymoat run -- make test`.

## Keys

```
keymoat scan                 # this project, and the Solana tool's default wallet
keymoat scan --history ~/code
keymoat keys move            # the keys in this project's env files
keymoat keys move ~/.config/solana/id.json
```

An EVM key becomes a standard encrypted keystore in `~/.foundry/keystores`, the one `--account NAME` reads. keymoat is not needed to use it afterwards.

A Solana keypair has no standard encrypted form, so it goes into keymoat's vault (scrypt and AES-256-GCM). Commands that pointed at the old file keep working: keymoat asks for the password and hands the tool the key for that one command. `keymoat keys export NAME --to FILE` writes it back out.

Before anything is removed, keymoat opens what it just encrypted and compares.

A key that sat in a plain file may already have been read. If the machine may have run untrusted code, move the funds to a new key.

## Network

Sandboxed commands go through a local proxy that passes a connection on only when its host is allowed:

- the built-in list: npm, crates.io, GitHub, Solidity and Solana toolchain downloads, block explorers, the public Solana clusters;
- hosts you typed on the command line (`--rpc-url https://...`);
- hosts in the project's own files (`foundry.toml`, `hardhat.config`, `Anchor.toml`, `.env`), once you have said yes to them. keymoat asks the first time, because a project file is something a dependency can rewrite;
- `keymoat allow HOST` for this project, `keymoat allow --global HOST` for all.

Local services (a local node, a test validator) are always reachable. `KEYMOAT_NET=open` lifts the restriction for one command.

## What is protected

Unreadable in every sandboxed run: `~/.ssh`, `~/.gnupg`, Foundry and other keystores, Solana keypairs in `~/.config/solana`, cloud and registry credentials (`~/.aws`, `~/.config/gcloud`, `~/.config/gh`, `~/.netrc`, `~/.npmrc` when it holds a token, `~/.cargo/credentials.toml`), the login keychain, browser profiles, wallet and password apps, chat apps, shell history, and every `.env` file outside the project.

Writable: the project, temporary folders and the toolchains' caches. Not the rest of your home folder, not shell start-up files, not `.git/hooks`.

Add your own in `~/.keymoat/config.json`:

```json
{ "protect": ["~/secrets"], "allowWrite": ["~/.my-tool/cache"], "allowHosts": ["rpc.mycompany.com"] }
```

`keymoat log` shows what was blocked, asked and allowed.

## Limits

keymoat narrows what untrusted code can do. It is not a guarantee, and these are the edges.

- **Only commands that go through keymoat are sandboxed.** An editor extension runs inside the editor, outside any sandbox, and can read every plain file you can. Against that, the defence is an encrypted keystore: `keymoat keys move`.
- **When a command signs, the code it runs can see the key it signs with.** A deploy script's dependencies run with the project's env files readable. keymoat asks first and keeps every other secret closed, but an encrypted keystore or a hardware wallet is what protects the key itself.
- **An allowed host is a way out.** A program that may reach GitHub or the npm registry can send what it can read there. It cannot read keys, so this limits the damage rather than removing the channel.
- **Solana deploys run with an open network.** `solana program deploy` talks to validators directly, which no proxy can carry. `anchor deploy` and tests on a live cluster therefore run without the network restriction (files stay protected).
- **Caches are writable.** A poisoned cache is run later, inside the sandbox again as long as the tools go through keymoat.
- **Tools that ignore proxy settings cannot connect out.** WebSocket RPC endpoints in particular. `keymoat allow` does not help there; `KEYMOAT_NET=open` does.
- **Private dependencies over SSH do not install**: no SSH key is in reach. Public GitHub repositories named by their SSH address are fetched over HTTPS automatically.
- **Global installs fail** (`npm install -g`, toolchain updates): they write outside the project. Run them with `KEYMOAT=off`.
- **macOS:** the system sandbox keymoat uses is marked deprecated by Apple. It works, and other developer tools rely on it.
- **Linux:** `.env` files of the projects next to this one are hidden; those of projects kept elsewhere stay readable (add their folders to `protect`). A local service started outside the sandbox is not reachable from inside it. Foundry, npm and Cargo are tested on Linux; the Solana and Anchor tools are not yet.
- **bun 1.2 and earlier** can hang behind any HTTP proxy while re-checking a cached package. Update bun, or run that install with `KEYMOAT_NET=open`.

## Troubleshooting

- **"Operation not permitted" / "EPERM" on a file**: the command tried to read a protected file or write outside the project. If it is a cache folder, add it to `allowWrite`.
- **A tool goes around keymoat**: `keymoat doctor` shows which. The PATH line must come after nvm, cargo and foundry in your shell's start-up file.
- **What the sandbox allowed for one run**: `KEYMOAT_DEBUG=1 forge build` keeps the rules file and prints where it is.
- **A pipeline**: there is no one to ask, so a signing command stops. Set `KEYMOAT_ASSUME_YES=1` there.

## Uninstall

```
keymoat uninstall
```

removes the tool links and the line in your shell's start-up file. Settings, the log and the vault stay in `~/.keymoat`; export any vault key before deleting that folder.

## Documentation

The [docs](docs/README.md) go further than this page: a guide per tool ([Foundry](docs/tools/foundry.md), [Hardhat and Node](docs/tools/node.md), [Solana and Anchor](docs/tools/solana.md)), [keys](docs/keys.md), [network](docs/network.md), [signing](docs/signing.md), [pipelines and editors](docs/automation.md), and the reference for [commands](docs/commands.md), [settings](docs/settings.md) and [what is protected](docs/protected.md).

To report a security problem, see [SECURITY.md](SECURITY.md).

## Development

```
go test ./...
```

The tests run real commands in the real sandbox, in a home folder made for the test. No dependency beyond the Go team's `x/crypto` and `x/term`.

## Licence

MIT
