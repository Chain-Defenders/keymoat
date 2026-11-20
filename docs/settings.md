# Settings

KeyMoat works without any settings. This page is the reference for when you need to change something.

## Environment switches

Set in front of one command, or exported for a shell or a pipeline step.

| Variable | Value | Effect |
|---|---|---|
| `KEYMOAT` | `off` | The command runs without KeyMoat: no sandbox, no confirmation. KeyMoat says so and logs it. |
| `KEYMOAT_KEYS` | `1` | The project's env files and key variables are visible to this command. Other secrets stay closed. Logged. |
| `KEYMOAT_NET` | `open` | No network limit for this command. Files stay protected. |
| `KEYMOAT_ASSUME_YES` | `1` | A signing command runs without asking. The confirmation is still printed. For pipelines. |
| `KEYMOAT_DEBUG` | `1` | macOS: keeps the sandbox's rules file for this run and prints where it is. |
| `KEYMOAT_HOME` | a folder | Where KeyMoat keeps its files, in place of `~/.keymoat`. |

```
KEYMOAT=off npm install -g typescript
KEYMOAT_KEYS=1 node scripts/release.js
```

KeyMoat also sets one variable itself: `KEYMOAT_ACTIVE=1` inside a sandboxed run. A tool started inside that run sees it and is not sandboxed a second time. Setting it yourself does nothing: KeyMoat checks that it really is inside a sandbox before believing it.

## config.json

`~/.keymoat/config.json`. Created the first time you answer a question or run `keymoat allow`. Every field is optional.

```json
{
  "allowHosts": ["rpc.mycompany.com", "*.internal.example"],
  "protect": ["~/secrets", "~/work/client-keys"],
  "allowRead": ["~/.npmrc"],
  "allowWrite": ["~/.my-tool/cache"],
  "projects": {
    "/Users/you/code/vault": {
      "hosts": ["eth-mainnet.g.alchemy.com"],
      "denied": ["rpc.unknown-host.org"]
    }
  },
  "vault": {
    "/Users/you/.config/solana/id.json": "main"
  }
}
```

| Field | Meaning |
|---|---|
| `allowHosts` | Hosts every project's commands may reach, on top of the built-in list. `*.example.com` covers the name and everything under it. Written by `keymoat allow --global`. |
| `protect` | More files and folders that sandboxed commands cannot read or write. `~` is your home folder. |
| `allowRead` | Paths to take **off** the protected list. This weakens the protection: use it only for a file a tool truly needs, such as `~/.npmrc` for a private registry. The login keychain cannot be opened this way. |
| `allowWrite` | More folders under your home folder that sandboxed commands may write, for a tool whose cache KeyMoat does not know. |
| `projects` | Per project (by its folder): the hosts you allowed (`hosts`) and the ones you refused (`denied`). Written when you answer the hosts question and by `keymoat allow`. Delete an entry from `denied` to be asked again. |
| `vault` | The path each Solana keypair had before it was moved into the vault, and its vault name. This is how commands that still point at the old path keep working. Written by `keymoat keys move`. |

A file that is not valid JSON stops KeyMoat with an error; it does not fall back to defaults silently.

Sandboxed commands cannot read or write this file.

## Files KeyMoat keeps

All in `~/.keymoat` (or `KEYMOAT_HOME`).

| Path | What |
|---|---|
| `bin/` | The tool links. |
| `config.json` | Settings, above. |
| `events.jsonl` | The log, one JSON object per line. Safe to delete. |
| `vault/NAME.json` | An encrypted Solana keypair. **Losing this file loses the key.** |
| `dev-wallet.json` | The throwaway wallet for local Anchor tests. Plain text on purpose: it holds nothing. |
| `run/` | Vault keys handed to a tool, for the length of one command. Empty otherwise. |

Outside that folder, KeyMoat writes:

- EVM keystores in `~/.foundry/keystores` (Foundry's own folder), when you move a key;
- one line in your shell's start-up file, with `keymoat install --shell`;
- a temporary rules file per sandboxed run, removed when the run ends.

## How a project is found

Several settings are per project. The project of a command is:

1. the git repository the current folder is in; or else
2. the nearest folder, going up, that has `foundry.toml`, `Anchor.toml`, a Hardhat configuration, `package.json` or `Cargo.toml`; or else
3. the current folder.

Your home folder is never a project. A command run there can write to temporary folders and caches only.

A monorepo is one project: the whole repository is writable, and hosts are allowed for all of it.

## Encryption

| What | Format |
|---|---|
| EVM key | Web3 Secret Storage version 3: scrypt (N = 131072, r = 8, p = 1) and AES-128-CTR, with a Keccak-256 check. The format Foundry, geth and most wallets read. |
| Solana keypair | KeyMoat's own file: scrypt with the same parameters, AES-256-GCM. The address is stored readable and bound to the encrypted key, so an entry whose address was edited does not open. |

Passwords are at least 8 characters. KeyMoat stores no password and cannot recover one.
