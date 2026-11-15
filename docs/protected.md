# What is protected

The exact lists, for people who want to check. Paths are relative to your home folder unless they start with `/`.

## Unreadable in every sandboxed run

A sandboxed command cannot read, list or write these, whatever the command is.

### On macOS and Linux

| Kind | Paths |
|---|---|
| Keys and wallets | `.ssh`, `.gnupg`, `.foundry/keystores`, `.brownie/accounts`, `.ethereum/keystore`, `.electrum`, `.bitcoin`, `.config/solana/id.json`, and any other Solana keypair file in `.config/solana` |
| Cloud and registry credentials | `.aws`, `.azure`, `.kube`, `.config/gcloud`, `.config/gh`, `.docker/config.json`, `.netrc`, `.git-credentials`, `.cargo/credentials`, `.cargo/credentials.toml`, `.config/hub`, `.terraform.d/credentials.tfrc.json`, `.vercel`, `.config/vercel`, `.fly`, `.config/doctl` |
| Password stores | `.password-store`, `.config/op`, `.config/Bitwarden CLI` |
| What you typed before | `.zsh_history`, `.bash_history`, `.zhistory`, `.local/share/fish/fish_history`, `.node_repl_history`, `.python_history`, `.psql_history` |
| keymoat's own | the vault, keys handed out for a run, `config.json`, the log |
| Yours | everything under `protect` in `config.json` |

`~/.npmrc` is added when it holds a token.

### macOS only

| Kind | Paths (under `Library/`) |
|---|---|
| The keychain | `Keychains`. Always closed; no setting opens it. |
| Browsers | `Application Support/Google`, `BraveSoftware`, `Firefox`, `Microsoft Edge`, `Arc`, `Chromium`, `Vivaldi`, `com.operasoftware.Opera`, `zen`; `Safari`, `Containers/com.apple.Safari`, `Cookies` |
| Wallet and password apps | `Application Support/Ledger Live`, `Exodus`, `Electrum`, `atomic`, `Phantom`, `Backpack`, `Frame`, `Bitwarden`, `1Password`; `Group Containers/2BUA8C4S2C.com.1password`, `Containers/com.bitwarden.desktop`; `Ethereum/keystore` |
| Chat and mail | `Application Support/Telegram Desktop`, `Slack`, `discord`; `Group Containers/6N38VWS5BX.ru.keepcoder.Telegram`; `Messages`, `Mail` |

Browser profiles are on the list because that is where browser wallets keep their vaults, and where session cookies are.

### Linux only

| Kind | Paths |
|---|---|
| Keyrings | `.local/share/keyrings`, `.pki` |
| Browsers | `.config/google-chrome`, `.config/chromium`, `.config/BraveSoftware`, `.mozilla`, `.config/microsoft-edge`, `.config/vivaldi`, `.config/opera` |
| Wallet and password apps | `.config/Ledger Live`, `.config/Exodus`, `.config/Electrum`, `.config/Frame`, `.config/Bitwarden`, `.config/1Password` |
| Chat and mail | `.local/share/TelegramDesktop`, `.config/Slack`, `.config/discord`, `.thunderbird` |

## Env files

| File | macOS | Linux |
|---|---|---|
| The project's env files that hold a wallet key | Hidden in commands that do not sign | Same |
| The project's other env files | Readable | Readable |
| Env files of any other project, anywhere | Hidden | Hidden for projects in the folders next to this one; readable elsewhere |
| `.env.example`, `.sample`, `.template`, `.dist`, `.defaults` | Readable | Readable |

"Env file" means a file named `.env` or `.env.SOMETHING`. A hidden env file also cannot be renamed, deleted or linked: a file that can be renamed can be read under its new name.

### Wallet files inside the project

In commands that do not sign, these are hidden like an env file that holds a key:

- a Solana keypair file (a JSON list of 64 numbers);
- a file named `.secret`, `.secrets`, `.mnemonic`, `mnemonic.txt`, `secret.txt`, `seed.txt`, `.private-key`, `private-key.txt` or `*.key`, when its whole content is a key or a seed phrase.

Not hidden: keypairs in folders named `test`, `tests`, `fixtures`, `__tests__`, `testdata`, `e2e` or `examples` (test fixtures), and a program's own keypair in `target/deploy`.

On Linux, add the folders where you keep other projects to `protect` if they are not next to the one you work in.

## The exceptions a command gets

| Command | Can also read |
|---|---|
| A signing command | The project's env files; the keystore, keypair or password file it names |
| `npm publish`, `login`, ... | `~/.npmrc`, `~/.yarnrc.yml`, `~/.bunfig.toml` |
| `cargo publish`, `login`, `owner`, `yank` | `~/.cargo/credentials.toml` |
| Any command with `KEYMOAT_KEYS=1` | The project's env files |

A named keystore is opened for reading only. It cannot be overwritten.

Which file a command names is not taken on trust, because the name can come from a project file or a script. keymoat opens **one file, never a folder**, and only if it is what it claims to be: a keystore directly inside `~/.foundry/keystores`, a Solana keypair file, or a vault key keymoat itself handed out. A keystore *name* (`--account`) must be a plain name with no path in it. `ETH_KEYSTORE_ACCOUNT` counts only when it is set in your own environment, not in a project's env file, and only for a command that signs. Anything else is refused with a message.

## Writable

A sandboxed command can write:

- **the project** (see [how a project is found](settings.md#how-a-project-is-found)), except `.git/hooks`;
- **temporary folders**. On macOS the command also gets a temporary folder of its own (`~/Library/Caches/keymoat/tmp`, set as `TMPDIR`; entries untouched for a week are removed);
- **the toolchains' caches** under your home folder:

| Tool | Paths |
|---|---|
| Foundry | `.foundry/cache`, `.svm`, `.local/share/svm`, `.solcx`, `.vvm`; macOS: `Library/Application Support/svm` |
| npm, pnpm, yarn, bun | `.npm`, `.pnpm-store`, `.local/share/pnpm/store`, `.local/state/pnpm`, `.yarn/berry`, `.yarn-cache`, `.bun/install/cache`, `.node-gyp`; macOS: `Library/pnpm/store`, `Library/Preferences/pnpm` |
| Hardhat | `.config/hardhat-nodejs`, `.local/share/hardhat-nodejs`; macOS: `Library/Preferences/hardhat-nodejs`, `Library/Application Support/hardhat-nodejs` |
| Cargo | `.cargo/registry`, `.cargo/git`, and Cargo's lock files |
| Solana | `.rustup/toolchains/solana`, and the `sbf/dependencies` folder of the installed Solana release (where the build tools link the compiler they download) |
| General | `.cache`; macOS: `Library/Caches` |
| Yours | everything under `allowWrite` in `config.json` |

Everything else under your home folder is read-only: shell start-up files, `~/.gitconfig`, editor settings and extensions, `~/.cargo/bin`, the tools themselves, and keymoat's own folder. `cargo install` is the one command that may write `~/.cargo/bin`.

Nothing outside this list is writable, on either system.

**Socket files** (how many tools talk to their own helper processes) can be made and used in the project and, on macOS, in the command's own temporary folder. Not in the system's shared temporary folders, where other programs, an editor among them, keep theirs.

## Not reachable at all

Beyond files, a sandboxed command on macOS cannot:

- start or script another app;
- run a program that has more rights than you (a setuid program);
- ask the keychain for a password;
- look a host name up directly (the proxy does it, for allowed hosts);
- connect anywhere outside the machine except through the proxy.

On Linux it runs with its own network, with no route out except the proxy.

## Checking for yourself

```
keymoat run -- sh -c 'cat ~/.ssh/id_ed25519; ls ~/.foundry/keystores; curl -m 5 https://example.com'
```

Each part fails. On macOS, `KEYMOAT_DEBUG=1` in front of any command keeps the rules file for that run and prints its path.
