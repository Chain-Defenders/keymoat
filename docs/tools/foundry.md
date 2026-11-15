# Foundry

`forge` runs your project's code and is sandboxed. `cast` talks to a chain and runs as it is; keymoat only steps in before it sends.

## What works without changes

```
forge build
forge test
forge test --fork-url mainnet
forge install foundry-rs/forge-std
forge fmt, snapshot, coverage, doc
```

- **Compiler downloads** come from `binaries.soliditylang.org`, which is allowed.
- **`forge install`** clones from GitHub over HTTPS. A dependency named by its SSH address (`git@github.com:...`) is fetched over HTTPS too.
- **Fork tests** need the RPC host. A name from `foundry.toml` (`--fork-url mainnet`) works once you have said yes to that project's hosts; a full URL on the command line works straight away. See [Network](../network.md).
- **`anvil` inside a run** works: a sandboxed command can listen on a local port and reach it.

`anvil` and `chisel` themselves are not put behind keymoat: they run no project code.

## What tests and scripts can see

Solidity code reaches the outside world through cheatcodes. Under keymoat:

| Cheatcode | In a build or test |
|---|---|
| `vm.envUint("PRIVATE_KEY")` and the other `vm.env*` | A wallet key is not in the environment: the call fails (or `vm.envOr` returns the default). Other variables from `.env` work. |
| `vm.readFile(".env")` | Fails when the file holds a key. |
| `vm.readFile` on a keystore or wallet file | Fails. |
| `vm.ffi` | The command runs inside the same sandbox: no keys, no free network. |
| `vm.rpc`, `vm.createFork` | Reaches allowed hosts only. |

This is what stops a malicious Solidity dependency from reading your deployer key during `forge test`.

If one of your own tests reads a key from the environment, give the test a throwaway key instead. The first local-node accounts are not treated as secrets, so `PRIVATE_KEY=0xac09...ff80` in `.env` stays visible.

## Deploying

```
forge script script/Deploy.s.sol --rpc-url mainnet --broadcast --account deployer
```

keymoat shows the confirmation, with the chain the endpoint reports, then runs the script in the sandbox with:

- the project's env files readable;
- the keystore `deployer` readable, and no other keystore;
- the network limited to allowed hosts, as always.

Foundry asks for the keystore password itself.

### Ways to sign, and what keymoat shows

| You pass | The confirmation says | Note |
|---|---|---|
| `--account NAME` | encrypted keystore "NAME" | Recommended. `keymoat keys move` creates one from a key in `.env`. |
| `--keystore PATH` | encrypted keystore PATH | |
| `--ledger`, `--trezor` | hardware wallet | The sandbox allows the USB device for this run. Not yet tested with a device: if it fails, run that deploy with `KEYMOAT=off` and tell us. |
| `--interactive` | typed in when asked | |
| `--private-key 0x...` | a private key on the command line | Warned about: it stays in shell history. |
| Nothing, with a key in `.env` | plain text: PRIVATE_KEY in .env | Warned about. |

`forge create` is asked about even without `--broadcast`, because older versions of forge deploy without it. `forge script --resume` is asked about too.

A keystore is named by a plain name: `--account deployer`, not a path.

### Dry runs

`forge script` without `--broadcast` is a simulation and is not asked about. It also does not see the project's keys. If your script reads `PRIVATE_KEY` from the environment, a dry run fails with a note from keymoat. Two ways out:

- **Better:** call `vm.startBroadcast()` with no argument and pass `--account NAME`. A dry run with `--account` can read that keystore.
- **Quick:** `KEYMOAT_KEYS=1 forge script script/Deploy.s.sol --rpc-url mainnet`.

### Verifying

`forge verify-contract` and `--verify` reach the explorers on the built-in list (Etherscan and its sister sites, Blockscout, Sourcify, Routescan). For another explorer, pass `--verifier-url` (allowed because you typed it) or `keymoat allow` its host.

## cast

| Command | What keymoat does |
|---|---|
| `cast call`, `balance`, `block`, `storage`, `run`, and other reads | Nothing. |
| `cast send`, `batch-send` (and their short names) | Shows the confirmation: chain, key, recipient, function and value. |
| `cast publish` | Shows the confirmation for the already signed transaction. |
| `cast erc20 transfer`, `approve`, `mint`, `burn` | Shows the confirmation. |
| `cast mktx`, `cast wallet sign`, `cast wallet sign-auth` | Shows the confirmation: these sign without sending, and the result can be used by whoever holds it. |
| Other `cast wallet` commands | Nothing. |

`cast` is not sandboxed, so it reads keystores and reaches any host as usual.

## foundry.toml

keymoat reads two things from it:

- `[rpc_endpoints]`, to resolve a name like `mainnet` to its URL for the confirmation, with `${VARIABLES}` filled in from the environment and the project's `.env`;
- every URL in the file, as hosts the project wants to reach.

It does not change the file.

## Known edges

- **WebSocket endpoints** (`wss://`) do not go through the network filter. Use the HTTPS endpoint for fork tests, or `KEYMOAT_NET=open`.
- **Private submodules over SSH** do not install: no SSH key is readable. Run that one `forge install` with `KEYMOAT=off`.
- **`foundryup`** is not put behind keymoat; update Foundry as usual.
