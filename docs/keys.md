# Keys

The sandbox keeps keys away from tools that run through keymoat. A key in a plain file is still readable by everything else on your machine: an editor extension, a script you run directly, a backup tool. This page is about getting keys out of plain files.

## Find them

```
keymoat scan
```

looks in the current project and at the Solana command-line tool's default wallet:

```
2 wallet keys in plain text:

  /Users/you/code/vault/.env:3
    DEPLOYER_PRIVATE_KEY: private key
    fix: keymoat keys move /Users/you/code/vault/.env

  /Users/you/.config/solana/id.json
    Solana keypair (the command-line tool's default wallet)
    fix: keymoat keys move /Users/you/.config/solana/id.json
```

It prints where a key is and what kind, never the key. It exits with code 1 when it finds something, so it can guard a pipeline.

| Option | Does |
|---|---|
| `keymoat scan DIR...` | Scans those folders instead of the current project. `keymoat scan ~/code` covers every project in it. |
| `--history` | Also looks through shell history for keys typed on a command line (`--private-key 0x...`, `PRIVATE_KEY=...`). |
| `--json` | Prints the findings as JSON. |

What it looks at:

- **env files** (`.env`, `.env.local`, `.env.production`, ...; not `.env.example` and the like);
- **project settings** (`hardhat.config.*`, `foundry.toml`, `truffle-config.js`, `Anchor.toml`) for a key written into the file;
- **Solana keypair files** (a JSON list of 64 numbers). A program's own keypair in `target/deploy/` is skipped: it names the program and holds no funds.

Dependency and build folders (`node_modules`, `lib`, `target`, `out`, ...) are skipped.

### What counts as a wallet key

keymoat goes by the shape of a value:

| Shape | Treated as |
|---|---|
| 64 hex digits, with or without `0x` | private key |
| 12, 15, 18, 21 or 24 lowercase words | seed phrase |
| 86 to 88 base58 characters | Solana secret key |
| A JSON list of 64 numbers | Solana keypair |
| 32 or 64 bytes in base64, only under a name that says it is a key (`PRIVATE`, `WALLET`, `SEED`, `KEYPAIR`, `SIGNER`, `DEPLOYER`, `MNEMONIC`) | key in base64 |

The variable's name then decides. An application's own secret is very often 64 random hex digits, the same shape as a private key, so a name that marks one (`API`, `WEBHOOK`, `CRON`, `SESSION`, `JWT`, `AUTH`, `TOKEN`, `PASSWORD`, a service's name such as `TELEGRAM` or `STRIPE`, ...) clears it, unless the name also says wallet (`PRIVATE`, `WALLET`, `SIGNER`, `DEPLOYER`, `SEED`, `MNEMONIC`, `KEYPAIR`). A name such as `PRIVATE_KEY`, `DEPLOYER`, `MNEMONIC` or `WALLET` makes it certain. A name that says it is something else (`SALT`, `MERKLE_ROOT`, `TX_HASH`, `DOMAIN_SEPARATOR`, `..._ID`) clears it. Any other name gives "possible private key", which is hidden from builds and tests all the same.

The keys every local node hands out (the first Anvil and Hardhat accounts, and the `test test ... junk` phrase) are public and are ignored.

## Move an EVM key

```
keymoat keys move
```

moves every private key in the current project's env files. `keymoat keys move path/to/.env` does one file.

```
/Users/you/code/vault/.env line 3: DEPLOYER_PRIVATE_KEY is a private key in plain text.
Name for its keystore [deployer]:
Password for the keystore:
Again:
Moved to /Users/you/.foundry/keystores/deployer and removed from .env.
  Sign with:      forge script ... --account deployer     (or cast send ... --account deployer)
  In scripts:     use vm.startBroadcast() with no argument in place of vm.envUint("DEPLOYER_PRIVATE_KEY")
  Its address:    cast wallet address --account deployer
```

What happened:

1. The key was encrypted with your password into a standard keystore file (Web3 Secret Storage, version 3) in `~/.foundry/keystores`. This is Foundry's own format and folder: `cast wallet list` shows it, and keymoat is not needed to use it.
2. keymoat opened the new keystore with your password and compared the result with the original. Only then did it touch the env file.
3. The line in `.env` became a comment saying where the key went.

### What changes in your scripts

Before:

```solidity
uint256 pk = vm.envUint("DEPLOYER_PRIVATE_KEY");
vm.startBroadcast(pk);
```

```
forge script script/Deploy.s.sol --rpc-url mainnet --broadcast
```

After:

```solidity
vm.startBroadcast();
```

```
forge script script/Deploy.s.sol --rpc-url mainnet --broadcast --account deployer
```

Foundry asks for the keystore's password when it signs.

### Seed phrases and Hardhat

keymoat does not move a seed phrase. Import it yourself with `cast wallet import NAME --interactive` (or `--mnemonic-path`), then delete the line.

For Hardhat, a Foundry keystore does not help. Use Hardhat's own encrypted store (`npx hardhat keystore set NAME` in Hardhat 3, or configuration variables in Hardhat 2) and delete the line from `.env`.

## Move a Solana keypair

```
keymoat keys move ~/.config/solana/id.json
```

```
/Users/you/.config/solana/id.json is a Solana keypair in plain text (address 8ZoK...nnKF).
Name for it in the vault [id]: main
Password for the key:
Again:
Moved to the vault as "main" and removed the plain file.
```

Solana has no standard encrypted keypair file, so the keypair goes into keymoat's own vault in `~/.keymoat/vault`, encrypted with scrypt and AES-256-GCM. As with an EVM key, the entry is opened and compared before the plain file is removed.

### Using it afterwards

Nothing in your settings has to change. The Solana tool's settings and your `Anchor.toml` still point at the old path. When a command needs that wallet, keymoat:

1. asks for the vault password;
2. writes the keypair to a private temporary file;
3. runs the command with that file;
4. overwrites and deletes the file when the command ends.

```
$ solana address
Password for the key "main":
8ZoKc4svqdFSRGpBTVm271qeX26AfnS2CEsiynwknnKF
```

You can also name a vault key directly:

```
solana transfer DEST 1 --keypair keymoat:main
anchor deploy --provider.wallet keymoat:main
```

Commands that do not use the wallet (`solana account ADDRESS`, `solana program show`, `solana cluster-version`) do not ask for the password. `solana address` and `solana balance` do, because they read the wallet to know its address.

This works only through keymoat. A tool run with `KEYMOAT=off`, or on a machine without keymoat, finds no file at the old path.

### Getting the plain file back

```
keymoat keys export main --to ~/backup/id.json
```

writes the keypair in plain text and leaves the vault entry in place. Do this before you remove keymoat or move to another machine.

## List what is encrypted

```
keymoat keys list
```

```
Foundry keystores (use with --account NAME):
  deployer
Solana keypairs in the keymoat vault (use with --keypair keymoat:NAME):
  main                 8ZoKc4svqdFSRGpBTVm271qeX26AfnS2CEsiynwknnKF
```

## After a move

- **Back up.** Losing the password, or the keystore file, loses the key. Keep a copy of the key or seed phrase offline.
- **Consider the key exposed.** It sat in a plain file until now. If this machine may ever have run untrusted code, move the funds to a new key. Deleting a file does not remove it from backups, snapshots or the disk's spare blocks.
- **Clean your history.** `keymoat scan --history` shows whether a key was ever typed on a command line.

## Better than a password

A keystore protects a key at rest. While a deploy runs, the key is in memory. For anything of value, sign with a hardware wallet (`--ledger`, `--trezor`, `usb://ledger`) or a multisig; keymoat passes those through and shows them on the confirmation.
