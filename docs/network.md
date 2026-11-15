# Network

A sandboxed command can talk to local services and to a list of allowed hosts. Everything else is refused. This page explains the list and how to change it.

## How it is enforced

For each sandboxed run, keymoat starts a small proxy on your machine and tells the tools to use it (through the usual `HTTPS_PROXY` variables). The sandbox then forbids every connection that leaves the machine except through that proxy.

- A request to an allowed host passes through unchanged. keymoat does not open or inspect encrypted traffic; it sees the host name only.
- A request to any other host is refused. When the command ends, keymoat names it:

  ```
  keymoat: blocked 2 connection attempts to telemetry.example.com
  keymoat: if npm should reach a host: keymoat allow HOST
  ```

- A program that ignores the proxy and connects directly gets nowhere: direct connections and name lookups are blocked by the sandbox itself.

Blocked hosts are also written to the log (`keymoat log`).

## Who is on the list

A host is allowed when it is in any of these.

### 1. The built-in list

What the toolchains themselves need:

| For | Hosts |
|---|---|
| Packages | `registry.npmjs.org`, `registry.yarnpkg.com`, `repo.yarnpkg.com`, `registry.npmmirror.com`, `nodejs.org`, `crates.io`, `*.crates.io`, `static.rust-lang.org`, `api.soldeer.xyz`, `*.soldeer.xyz`, `soldeer-revisions.s3.amazonaws.com` |
| Source | `github.com`, `api.github.com`, `codeload.github.com`, `*.githubusercontent.com`, `gitlab.com`, `bitbucket.org` |
| Compilers and toolchains | `binaries.soliditylang.org`, `solc-bin.ethereum.org`, `release.anza.xyz`, `release.solana.com`, `*.vyperlang.org` |
| Contract verification | `*.etherscan.io`, `*.basescan.org`, `*.arbiscan.io`, `*.polygonscan.com`, `*.bscscan.com`, `*.snowtrace.io`, `*.blockscout.com`, `sourcify.dev`, `*.sourcify.dev`, `*.routescan.io` |
| Solana's public clusters | `api.mainnet-beta.solana.com`, `api.devnet.solana.com`, `api.testnet.solana.com` |
| Signature lookups | `api.openchain.xyz`, `*.4byte.directory`, `sig.eth.samczsun.com` |

`*.example.com` covers `example.com` and everything under it.

### 2. Hosts you typed

A URL on the command line is your own intent, so its host is allowed for that run:

```
forge test --fork-url https://my-node.example.com/v1/abc
```

A *name* that refers to `foundry.toml` (`--fork-url mainnet`) is not a typed host; it falls under the next rule.

### 3. The project's own hosts, once you said yes

keymoat reads the hosts a project points at:

- `foundry.toml`: `[rpc_endpoints]` and any other URL, with `${VARIABLES}` filled in;
- `hardhat.config.*`: any URL;
- `Anchor.toml`: any URL;
- the project's env files: any value that is a URL.

The first time it sees a host there, it asks:

```
keymoat: this project's files point at 2 hosts keymoat has not seen before:

  eth-mainnet.g.alchemy.com                (.env)
  rpc.unknown-host.org                     (foundry.toml)

Let this project's commands connect to them? [y/N]
```

`y` allows them for this project from now on. `n` keeps them blocked, and keymoat does not ask about them again.

**Why it asks.** A project file is something a dependency can rewrite. If hosts in project files were trusted automatically, an install script could add its own server to `foundry.toml` and the next run would let it through. Asking once means a new host is always something you saw.

Without a terminal (an editor, a pipeline) there is no one to ask. New hosts are then not allowed and not recorded, so the question comes up the next time you run a command in a terminal.

### 4. Hosts you allowed by hand

```
keymoat allow rpc.mycompany.com            # for this project
keymoat allow --global rpc.mycompany.com   # for every project
```

You can pass a full URL; keymoat takes the host from it. Allowing a host by hand also undoes an earlier `n`.

### 5. The Solana tool's own endpoint

The RPC URL in the Solana command-line tool's settings (`solana config get`) is allowed. That file is yours, outside any project.

## Local services

`localhost`, `127.0.0.1` and other local addresses are always reachable: a local node, a test validator, a database for tests. A sandboxed command can also listen on a port, so `anvil` or `hardhat node` started inside a run works.

## Lifting the limit for one command

```
KEYMOAT_NET=open forge script script/Fetch.s.sol
keymoat run --open-net -- ./scripts/sync.sh
```

The files stay protected; only the network limit is lifted.

Some commands always run with the network open, because their traffic cannot go through a proxy:

- `anchor deploy`, `upgrade`, `migrate`, `run` and `test`, when the cluster is not a local one. The confirmation says so.

## What does not go through

| Case | Why | What to do |
|---|---|---|
| WebSocket RPC endpoints (`wss://`) | Most tools do not send WebSocket traffic through a proxy. | Use the HTTPS endpoint, or `KEYMOAT_NET=open` for that command. |
| Private git dependencies over SSH | No SSH key is readable in the sandbox. | Install them with `KEYMOAT=off`, or use an HTTPS URL with a token the tool is given. |
| A corporate proxy you already use | keymoat's proxy connects directly and replaces your proxy settings inside the sandbox. | `KEYMOAT_NET=open`. |

Public GitHub repositories named by their SSH address (`git@github.com:org/repo`) are fetched over HTTPS automatically, so most `forge install` and git dependencies work unchanged.

## Reading the log

```
keymoat log
```

```
2026-11-12 11:34  blocked-host   npm     example.org
2026-11-12 12:00  blocked-host   npx     www.google-analytics.com
```

A blocked host you do not recognise after an install is worth a look: it is exactly what keymoat is there to show.

## Limits

An allowed host is still a way out. A program that may reach GitHub can send data to GitHub. It cannot read your keys, so what it can send is limited; see the [threat model](threat-model.md).
