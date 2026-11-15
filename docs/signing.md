# Signing

Most commands never need a key and never get one. A command that signs is different: keymoat shows it to you first, and gives it the one key it names.

## The confirmation

```
keymoat: this command signs with a real key.

  What       Run a script that sends transactions (forge script --broadcast)
  Network    Base (chain 8453) through mainnet.base.org
  Signs with encrypted keystore "deployer"
  Target     script/Deploy.s.sol

Type "yes" to continue:
```

| Line | Meaning |
|---|---|
| **What** | The action, in words. |
| **Network** | For EVM chains, keymoat asks the endpoint which chain it is and shows the answer next to the host. If the endpoint does not answer, the line says so and the network is treated as live. For Solana, the cluster's name. |
| **Signs with** | Where the signature comes from: a named keystore, a hardware wallet, a vault key, or a key in plain text (named, never shown). |
| **Target** | The script, contract or recipient. |
| **Note** | A warning, for example that the key is on the command line or in a plain file. |

### How you answer

| Network | Answer needed |
|---|---|
| A local node (`localhost`, `127.0.0.1`) | None. No question is asked. "Local" goes by the address, not by the chain id: an endpoint elsewhere that reports a local node's id (1337, 31337) is treated as live. |
| A known test network (Sepolia, Holesky, Hoodi, Base Sepolia, Arbitrum Sepolia, OP Sepolia, Polygon Amoy, Fuji, BNB testnet; Solana devnet and testnet) | `y` |
| Anything else, including a chain keymoat does not know and an endpoint that did not answer | the full word `yes` |

Asking for help (`--help`, `-h`) is never asked about. Anything else, including just pressing Enter, declines. A declined command is not run and exits with code 1, so a script that called it stops too.

Confirmed and declined commands are written to the log (`keymoat log`), with any key on the command line removed.

## Which commands are asked about

### Foundry

| Command | Asked | Keys it can read |
|---|---|---|
| `forge script ... --broadcast` or `--resume`; `forge create` (with or without `--broadcast`) | Yes | The project's env files; the keystore named with `--account` or `--keystore`; a `--password-file` |
| `forge script` without `--broadcast` (a dry run) | No | The keystore you name, if any. Not the env files. |
| `cast send`, `batch-send`, `publish`; `cast erc20 transfer`, `approve`, `mint`, `burn` | Yes | `cast` runs outside the sandbox |
| `cast mktx` (signs without sending); `cast wallet sign`, `sign-auth` | Yes, always with the full word `yes`: a signature can bind you as much as a transaction | The same |
| `forge build`, `test`, `install`, everything else | No | None |

A dry run that reads `PRIVATE_KEY` from the environment fails under keymoat, because the key is hidden. Either run it with `KEYMOAT_KEYS=1`, or better, move the key into a keystore and pass `--account`.

### Hardhat and Node scripts

| Command | Asked | Keys it can read |
|---|---|---|
| `npx hardhat ... --network NAME`, where NAME is not `hardhat` or `localhost` | Yes | The project's env files |
| `npm run SCRIPT` (also `yarn SCRIPT`, `pnpm SCRIPT`), when the script's text deploys: `hardhat ... --network`, `--broadcast`, `anchor deploy`, `solana program deploy`, `cast send` | Yes | The project's env files |
| `npm publish`, `login`, `whoami` and other registry commands | No | The registry token (`~/.npmrc`) |
| `npm install`, `npm test`, `npx hardhat compile`, everything else | No | None |

keymoat reads the script's text from `package.json`; it cannot see what a JavaScript file does. A deploy hidden inside `node scripts/release.js` is not recognised, runs without keys, and fails. Run it with `KEYMOAT_KEYS=1`.

### Solana

| Command | Asked |
|---|---|
| `solana transfer`, `solana program deploy` and the other `program` and `program-v4` commands that write, stake, vote and nonce account commands, `address-lookup-table`, `feature activate` | Yes |
| `spl-token` commands other than `accounts`, `balance`, `supply`, `display`, `address`, `account-info`, `multisig-info` | Yes |
| `solana balance`, `account`, `config`, `program show`, `program dump`, and other reads | No |

`solana` and `spl-token` run outside the sandbox: they run no project code.

### Anchor

| Command | Asked | Wallet |
|---|---|---|
| `anchor test`, `anchor localnet` on a local validator | No | A throwaway wallet. See below. |
| `anchor deploy`, `upgrade`, `migrate`, `run`, `idl init`, `idl upgrade` and other IDL commands that write | Yes, unless the cluster is local | The wallet from `Anchor.toml` or `--provider.wallet` |
| `anchor test` against devnet, testnet or mainnet | Yes | The same |
| `anchor build`, `keys`, `idl build`, `expand`, `clean`, `init` | No | None |

### Cargo

`cargo publish`, `login`, `owner` and `yank` can read the crates.io token. Nothing else can.

## The throwaway wallet

`anchor test` on a local validator needs a wallet to pay for transactions. By default that is your real one from `Anchor.toml`, readable by every test and every test dependency.

keymoat gives those runs a wallet of its own (`~/.keymoat/dev-wallet.json`) that has never held anything:

```
keymoat: tests run with a throwaway wallet, not your own (pass --provider.wallet to choose one)
```

The local validator funds it at start-up, so tests work as before. Your real wallet stays unreadable for the whole run. If your tests depend on a particular address, pass `--provider.wallet PATH`.

## Letting a command see keys

When keymoat does not recognise a signing command, the command fails and keymoat says why:

```
keymoat: PRIVATE_KEY was hidden from this command, like every key. If the command has to sign, run it with KEYMOAT_KEYS=1.
```

```
KEYMOAT_KEYS=1 node scripts/release.js
keymoat run --keys -- make deploy
```

This makes the project's env files and your shell's key variables visible to that one command. Everything else stays closed: other projects' env files, keystores you did not name, SSH keys. The use is written to the log.

## Without a terminal

In a pipeline or an editor there is no one to ask, so a signing command does not run. To let it through:

```
KEYMOAT_ASSUME_YES=1 forge script script/Deploy.s.sol --rpc-url mainnet --broadcast --account deployer
```

The confirmation is still printed, so the pipeline's log shows what was signed. See [Pipelines and editors](automation.md).

## What the confirmation cannot tell you

It shows the command, the chain and the key. It does not simulate the transaction or read the script. A script that deploys something other than what you expect looks the same. Review deploy scripts, and for anything of value sign with a hardware wallet or a multisig that shows the transaction itself.
