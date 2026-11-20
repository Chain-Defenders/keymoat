# Solana and Anchor

`anchor` and `cargo` run your project's code and are sandboxed. `solana` and `spl-token` run no project code; KeyMoat only steps in before they sign.

The Solana wallet deserves attention: by default it is one plain file, `~/.config/solana/id.json`, used for local tests and for mainnet alike, and readable by every build script and test dependency. KeyMoat changes that in three ways.

## 1. Builds cannot read the wallet

```
anchor build
cargo build-sbf
cargo test
```

A Rust build runs other people's code at compile time: build scripts and procedural macros. Inside the sandbox they cannot read the wallet, any other keypair in `~/.config/solana`, or anything else on the [protected list](../protected.md).

Crates come from crates.io and the Solana toolchain from `release.anza.xyz` and GitHub, all allowed. The first build with a new toolchain version downloads the Solana compiler into `~/.cache/solana`; that works inside the sandbox.

## 2. Local tests get a throwaway wallet

```
anchor test
```

```
keymoat: tests run with a throwaway wallet, not your own (pass --provider.wallet to choose one)
```

On a local validator, KeyMoat runs the tests with its own wallet (`~/.keymoat/dev-wallet.json`), which has never held anything. Anchor funds it when it starts the validator, so tests behave as before. Your real wallet is unreadable for the whole run, including from the TypeScript tests and their dependencies.

This applies to `anchor test` and `anchor localnet` when the cluster is local (`localnet` in `Anchor.toml`, or a `localhost` URL) and you did not pass `--provider.wallet`.

If your tests expect a particular address, or you run against a validator you started yourself with `--skip-local-validator`, pass the wallet you want:

```
anchor test --skip-local-validator --provider.wallet ./tests/payer.json
```

## 3. Deploys are confirmed, with one key

```
anchor deploy --provider.cluster mainnet
```

```
keymoat: this command signs with a real key.

  What       anchor deploy
  Network    mainnet-beta
  Signs with /Users/you/.config/solana/id.json (address 8ZoK...nnKF)
  Note       The network is not restricted for this command: a Solana deploy talks to validators directly.
  Note       This wallet file is plain text. `keymoat keys move /Users/you/.config/solana/id.json` encrypts it.

Type "yes" to continue:
```

| Anchor command | Asked | Wallet readable |
|---|---|---|
| `build`, `keys`, `expand`, `clean`, `init`, `new`, `idl build`, `idl parse`, `idl fetch` | No | No |
| `test`, `localnet` on a local validator | No | The throwaway wallet only |
| `deploy`, `upgrade`, `migrate`, `run`, `idl init`, `idl upgrade`, `idl set-authority`, `idl close` | Yes, unless the cluster is local | Yes |
| `test` on devnet, testnet or mainnet | Yes | Yes |

Devnet and testnet take a `y`; mainnet and any other endpoint take the full word `yes`.

**The network is open for live deploys.** A Solana deploy sends transactions straight to validators, which a proxy cannot carry, so these commands run without the network limit. Files stay protected: the wallet you named is readable and nothing else on the protected list is.

Note what that means for `anchor test`, `run` and `migrate` on a live cluster: project scripts run with the wallet readable and the network open. KeyMoat says so on the confirmation. Use a wallet that holds only what that run needs.

## Encrypting the wallet

```
keymoat keys move ~/.config/solana/id.json
```

moves the keypair into KeyMoat's encrypted vault and removes the plain file. Nothing in `Anchor.toml` or the Solana tool's settings has to change: when a command needs that wallet, KeyMoat asks for the password and hands the tool the keypair for that one command.

```
$ solana transfer DEST 1
keymoat: this command signs with a real key.
  ...
  Signs with keymoat vault "main"

Type "yes" to continue: yes
Password for the key "main":
```

You can also name a vault key anywhere a keypair path goes:

```
solana program deploy target/deploy/app.so --keypair keymoat:main
anchor deploy --provider.wallet keymoat:main
```

[Keys](../keys.md) has the details, including how to get the plain file back.

## solana and spl-token

These run outside the sandbox.

| Command | Asked |
|---|---|
| `solana transfer` | Yes |
| `solana program deploy`, `write-buffer`, `upgrade`, `set-upgrade-authority`, `close`, `extend` | Yes |
| Stake, vote and nonce account commands that write | Yes |
| `solana address-lookup-table create`, `extend`, `close`; `solana feature activate` | Yes |
| `spl-token transfer`, `mint`, `burn`, `create-token`, `authorize`, and other commands that write | Yes |
| `solana balance`, `account`, `config`, `cluster-version`, `program show`, `program dump`, `logs` | No |
| `spl-token accounts`, `balance`, `supply`, `display` | No |

The cluster comes from `-u` / `--url` or from the tool's settings; the keypair from `-k` / `--keypair` or the settings. A hardware wallet (`usb://ledger`) is shown as such.

## Cargo

| Command | Under KeyMoat |
|---|---|
| `cargo build`, `test`, `check`, `run`, `build-sbf`, `test-sbf` | Sandboxed, no keys. |
| `cargo install NAME` | Sandboxed; may also write `~/.cargo/bin`. |
| `cargo publish`, `login`, `owner`, `yank` | Sandboxed; may read the crates.io token. |

`cargo` is behind KeyMoat for every Rust project, not only Solana ones. An editor's Rust language server usually calls `cargo` on its own; see [Pipelines and editors](../automation.md#in-an-editor).

## Known edges

- **A pinned Rust version that is not installed yet** (`rust-toolchain.toml`) cannot be installed from inside a build. Run `rustup toolchain install` in the project's folder once, then build.
- **Toolchain installs and updates** (`rustup update`, `agave-install update`, `avm install`) write outside the project. Run them outside KeyMoat; they are not behind it anyway, except through `cargo install`.
- **Tests against a validator outside the sandbox on Linux** cannot reach it: on Linux a sandboxed command has its own network. Let `anchor test` start the validator, or use `KEYMOAT_NET=open`.
- **Linux:** the Solana and Anchor tools have been tested under KeyMoat on macOS only so far.
- **Old Anchor templates** may not build with today's crates. That is unrelated to KeyMoat: the same error appears without it.
