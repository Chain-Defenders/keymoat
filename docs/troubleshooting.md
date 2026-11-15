# Troubleshooting

Find the message or the symptom, then the fix. When in doubt, two commands tell you most of what you need:

```
keymoat doctor      # is it set up right
keymoat log         # what it blocked and asked
```

To see whether keymoat is the cause at all, run the command once without it: `KEYMOAT=off COMMAND`.

## Messages from keymoat

### `blocked N connection attempts to HOST`

A sandboxed command tried to reach a host that is not allowed.

- **You know the host** (your RPC provider, a private registry): `keymoat allow HOST`, or `keymoat allow --global HOST` for every project.
- **You do not know it:** leave it blocked, and find out which dependency wanted it. This is what keymoat is for.
- **Analytics** (`www.google-analytics.com` from Hardhat, for instance): harmless to leave blocked.

### `X was hidden from this command, like every key`

The command failed, and a wallet key in `.env` or your environment was not visible to it. If the command is supposed to sign:

```
KEYMOAT_KEYS=1 COMMAND
```

If it is a build or a test, it should not need a real key: give it a throwaway one, or make the configuration tolerate a missing key ([Hardhat example](tools/node.md#your-configuration-and-hidden-keys)).

The message appears after any failed command where a key was hidden, so it can show up when the failure has another cause. Read the tool's own error first.

### `there is no terminal to confirm on, so the command was not run`

A signing command ran where no one can answer: a pipeline, an editor task, a command with its input redirected. In a pipeline, set `KEYMOAT_ASSUME_YES=1` on that step. See [Pipelines and editors](automation.md).

### `this project's files point at N hosts keymoat has not seen before`

Not an error: the one-time question about a project's RPC hosts. See [Network](network.md#3-the-projects-own-hosts-once-you-said-yes). If you answered `n` by mistake, `keymoat allow HOST` fixes it.

### `TOOL is not installed (it is not on the PATH)`

keymoat has a link for the tool but cannot find the real one. The tool was removed, or the folder it lives in is no longer on your `PATH`. Reinstall the tool, then run `keymoat install` again.

### `bubblewrap (the bwrap command) is not installed`

Linux: `sudo apt install bubblewrap` (or your distribution's package).

### `no terminal to ask for a password on`

A command needed a key from the vault and there is no terminal. Vault keys cannot be used in pipelines; use a keypair the pipeline provides as a file, passed with `--keypair`.

### `not opening PATH for this command: it is in a protected folder and is not a key file`

The command named a key, keystore or password file that sits in a protected folder and is not a keystore in `~/.foundry/keystores` or a Solana keypair. keymoat opens one real key file, never a folder and never some other protected file. If the file really is your key, move it into `~/.foundry/keystores` (or use `keymoat keys move`).

### `this command changed the repository's git settings to run a program`

A command added a setting to `.git/config` that makes git run a program. Unless you did that yourself, remove it before your next git command and find out which dependency did it. See the [threat model](threat-model.md).

### `KEYMOAT_ACTIVE is set, but this is not a keymoat sandbox: ignoring it`

Something set that variable outside a sandbox, where it has no business. keymoat ignores it and protects the command as usual. Find where it is set (a project's direnv file, a shell start-up file) and remove it.

## Errors from the tool

### `Operation not permitted`, `EPERM`, `Permission denied`, `Read-only file system`

The command tried to read a protected file or write outside the project and the caches.

| What it was doing | Fix |
|---|---|
| Writing a cache keymoat does not know | Add the folder to `allowWrite` in [settings](settings.md). |
| A global install (`npm install -g`) or a toolchain update | Run it with `KEYMOAT=off`. |
| Reading `~/.npmrc` for a private registry | Add `"allowRead": ["~/.npmrc"]`. |
| Reading a keystore or wallet in a build or test | That is the protection working. If the command really signs, see `KEYMOAT_KEYS=1` and [Signing](signing.md). |
| Reading `.env` | The file holds a wallet key and is hidden; its other variables are in the environment. |

On macOS, `KEYMOAT_DEBUG=1 COMMAND` keeps the rules file for the run and prints its path, so you can see exactly what was allowed.

### `could not create temp file ~/.rustup/tmp/...: Operation not permitted`

The project pins a Rust version (a `rust-toolchain.toml` file) that is not installed yet, and rustup tried to install it in the middle of the build. Installing a compiler writes outside the project, which the sandbox does not allow: a build must not be able to replace your compilers. Install it once yourself, from the project's folder, then build again:

```
rustup toolchain install
```

### `Could not resolve host`, `ENOTFOUND`, `failed to lookup address`, a connection that hangs or times out

The tool tried to connect without the proxy, and the sandbox stopped it.

- **A WebSocket endpoint** (`wss://`): use the HTTPS endpoint, or `KEYMOAT_NET=open`.
- **A script using Node's `fetch` on an older Node:** update Node, or `KEYMOAT_NET=open`.
- **A tool that ignores proxy settings:** `KEYMOAT_NET=open` for that command.

### `bad certificate format`, certificate errors

Should not happen on a current version. If it does on macOS, run with `KEYMOAT_DEBUG=1` and open an issue with the tool's name.

### `environment variable "PRIVATE_KEY" not found` (Foundry), `Invalid account` (Hardhat)

The key is hidden. See [the message above](#x-was-hidden-from-this-command-like-every-key), and for dry runs, [Foundry](tools/foundry.md#dry-runs).

### `Permission denied (publickey)`, a git dependency that will not clone

No SSH key is readable in the sandbox. Public GitHub repositories are switched to HTTPS automatically; a private one is not reachable. Run that install with `KEYMOAT=off`.

### `bwrap: setting up uid map: Permission denied`

Linux: unprivileged user namespaces are off. See [Platforms](platforms.md#requirements).

## Symptoms

### A tool goes around keymoat

`keymoat doctor` shows `runs directly from ...`. The folder `~/.keymoat/bin` is not first on your `PATH`.

- Make sure the keymoat line is the **last** line of your shell's start-up file, after nvm, cargo, foundry and bun add theirs.
- Open a new terminal after changing it.
- **nvm:** `nvm use` puts Node's folder first again for that terminal. Open a new terminal, or run `export PATH="$HOME/.keymoat/bin:$PATH"` again.
- **A tool installed after keymoat** has no link yet: run `keymoat install` again.

### `bun install` hangs at "Resolving dependencies"

bun 1.2 and earlier hangs behind any HTTP proxy while re-checking a cached package. Update bun, or `KEYMOAT_NET=open bun install`.

### A local node is not reachable (Linux)

On Linux a sandboxed command has its own network. Start the node in the same command, or use `KEYMOAT_NET=open`. See [Platforms](platforms.md#linux).

### `anchor test`: "Test validator does not look started"

Seen now and then on the first run after a while, when the validator takes longer than Anchor waits for it. Run it again, or raise the wait in `Anchor.toml`:

```toml
[test]
startup_wait = 15000
```

### `anchor test` fails with insufficient funds

The tests most likely ran with the throwaway wallet against a validator that did not fund it, usually one you started yourself. Pass the wallet you want: `anchor test --skip-local-validator --provider.wallet PATH`.

### Commands feel slower

keymoat adds about a tenth of a second per command. If it is much more, the project's files may be slow to walk: keymoat reads env files up to three folders deep on each run.

### The editor's test runner or language server stopped working

It runs your tools without a terminal. If it needs a host that was never approved, run any command for that project once in a terminal and answer the question. See [Pipelines and editors](automation.md#in-an-editor).

### I forgot a keystore or vault password

There is no recovery. Restore the key from your own backup (the original key or seed phrase) and move it again.

### I want my Solana keypair back as a file

`keymoat keys export NAME --to FILE`.

## Still stuck

Open an issue with:

- the command you ran, and the same command's result with `KEYMOAT=off`;
- the output of `keymoat doctor` and `keymoat version`;
- your system and its version.

Leave out keys, passwords and RPC URLs that contain a key.
