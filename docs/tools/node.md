# Hardhat and Node

`npm`, `npx`, `pnpm`, `pnpx`, `yarn`, `bun` and `bunx` are sandboxed. Installing a package is the most common way untrusted code gets to run on a developer's machine: every dependency can ship an install script.

`node` itself is not put behind KeyMoat by default (see [below](#node-itself)).

## Installs

```
npm install
pnpm add viem
yarn
bun install
```

An install script runs inside the sandbox:

- it cannot read your keys, your SSH folder or your browser profiles;
- it cannot read a wallet key in the project's `.env`, or find one in the environment;
- it can reach the registry and GitHub, and nothing else unless allowed;
- it can write to the project and the package manager's cache, not to the rest of your home folder.

When an install script tries to reach its own server, you see it:

```
keymoat: blocked 1 connection attempt to collect.example.net
```

### Your registry token

If `~/.npmrc` holds a token, it is hidden from installs: a stolen npm token is how package worms spread. The commands that need it can read it: `publish`, `login`, `adduser`, `logout`, `whoami`, `token`, `owner`, `access`, `dist-tag`, `deprecate`, `unpublish`, `team`, `org`, `profile`.

If you install from a **private registry** that needs the token, allow it in `~/.keymoat/config.json`:

```json
{ "allowRead": ["~/.npmrc"], "allowHosts": ["npm.mycompany.com"] }
```

### Global installs

`npm install -g` writes outside the project and fails under KeyMoat. Run it with `KEYMOAT=off npm install -g NAME`.

## Hardhat

```
npx hardhat compile
npx hardhat test
npx hardhat node
```

work unchanged. Compiler downloads are allowed. Hardhat's analytics call is not on the list and is blocked.

### Your configuration and hidden keys

A common configuration reads the key at load time:

```js
accounts: [process.env.PRIVATE_KEY]
```

Under KeyMoat, `PRIVATE_KEY` is not set during `compile` and `test`, and Hardhat rejects the undefined account. Make the configuration tolerate a missing key:

```js
accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : []
```

Better, take the key out of `.env` altogether with Hardhat's own encrypted store (`npx hardhat keystore set` in Hardhat 3, configuration variables in Hardhat 2).

### Deploying

```
npx hardhat run scripts/deploy.ts --network mainnet
npx hardhat ignition deploy ignition/modules/Token.ts --network sepolia
```

Any Hardhat command with `--network NAME`, where NAME is not `hardhat`, `localhost`, `local`, `anvil` or `default`, is treated as signing:

```
keymoat: this command signs with a real key.

  What       Run Hardhat against a live network
  Network    mainnet
  Signs with plain text: PRIVATE_KEY in .env
```

KeyMoat shows the network's *name*: the URL is inside your configuration, which is code, so the chain is not looked up. Check the name.

After you confirm, the command runs in the sandbox with the project's env files readable. The RPC host must be allowed: KeyMoat finds URLs written in `hardhat.config.*` and in `.env` and asks about them once.

## Package scripts

`npm run NAME`, `yarn NAME` and `pnpm NAME` are looked up in `package.json`. If the script's text deploys, the command is treated as signing:

| The script contains | Treated as |
|---|---|
| `hardhat ... --network NAME` (not a local name) | signing, on NAME |
| `--broadcast` | signing |
| `anchor deploy`, `anchor upgrade`, `anchor migrate` | signing |
| `solana program deploy`, `cast send` | signing |
| anything else | a normal run, no keys |

KeyMoat reads the script's text and nothing more. A script like `"release": "node scripts/release.js"` is not recognised as a deploy. It runs without keys and fails; run it with `KEYMOAT_KEYS=1 npm run release`.

## npx and one-off packages

`npx some-package` downloads and runs a package you may never have seen. Under KeyMoat it runs with no keys and a limited network, which is the right default for that.

## node itself

`node script.js` is not sandboxed unless you ask:

```
keymoat run -- node scripts/analyse.js
```

or put `node` behind KeyMoat permanently:

```
keymoat install --tools node
```

This is off by default because editors and many tools start `node` for their own purposes.

## Environment files

Frameworks load `.env`, `.env.local` and friends themselves. When one of those files holds a wallet key, KeyMoat hides the whole file from builds and tests and passes its other variables through the environment. Two effects worth knowing:

- a variable passed this way wins over the same variable in another env file, because frameworks give the real environment priority;
- env files in sub-folders are hidden when they hold a key, but only files in the folder you run from and in the project's top folder have their other variables passed through.

An env file with no wallet key in it is left alone.

## Known edges

- **bun 1.2 and earlier** can hang behind any HTTP proxy while re-checking a cached package. Update bun, or run that install with `KEYMOAT_NET=open bun install`.
- **Node's own `fetch`** follows the proxy only on recent Node versions (KeyMoat sets `NODE_USE_ENV_PROXY=1`). On older versions a script's `fetch` fails to connect; libraries that handle proxies themselves (npm, Hardhat's downloader) are not affected.
- **yarn 1** prints "Cannot find a suitable global folder". It is a warning; the install succeeds.
- **Native modules** that download prebuilt binaries from a host not on the list are blocked; allow the host the message names.
