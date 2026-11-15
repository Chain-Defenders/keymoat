# Changelog

## 0.1.0 (not yet released)

First version. keymoat replaces forge-guard, which tried to detect key theft as it happened; keymoat prevents it.

- **Sandbox** for `forge`, `anchor`, `cargo`, `npm`, `npx`, `pnpm`, `pnpx`, `yarn`, `bun` and `bunx`: keys, credentials, browser profiles and shell history are unreadable; only the project and the toolchains' caches are writable. macOS (the system sandbox) and Linux (bubblewrap).
- **Hidden keys:** a wallet key in the project's env files or in the environment is not visible to builds, tests and installs; the rest of the env file still is.
- **Network allowlist** through a local proxy, with a one-time question about the hosts a project's files name.
- **Confirmation before signing** for `forge script --broadcast`, `forge create`, `cast send`, Hardhat on a live network, deploy scripts, `solana`, `spl-token` and `anchor`. The chain is asked of the endpoint.
- **Throwaway wallet** for `anchor test` on a local validator.
- **`keymoat scan`** for wallet keys in plain text, **`keymoat keys move`** into a Foundry keystore (EVM) or keymoat's encrypted vault (Solana), `keys list`, `keys export`.
- **`keymoat install`**, `uninstall`, `doctor`, `run`, `allow`, `extensions`, `log`.

Hardening from the pre-release review:

- A keystore name or path can no longer be steered at another folder; one real key file is opened, never a folder.
- Hidden env files cannot be renamed, linked or deleted; wallet files kept in a project are hidden too.
- Writing is allowed place by place (project, caches, temporary folders), no longer everywhere outside the home folder.
- `KEYMOAT_ACTIVE` is believed only inside a real sandbox.
- More signing commands are asked about: `forge script --resume`, `forge create`, `cast`'s short names, `mktx`, `erc20`, `wallet sign`.
- A warning when a command plants a program in the repository's git settings.

From real projects and the first real install:

- Built-in hosts: Soldeer's download host and Google Fonts (which Next.js builds fetch).
- An application's own 64-digit secrets (API keys, webhook and session secrets) are no longer taken for wallet keys.
- The test validator is started once before a local `anchor test`, so a slow first start does not fail the run.
- Idle proxy connections are closed after ten minutes; `keymoat uninstall` restores the shell start-up file exactly.

Known gaps: hardware wallets are untested; the Solana and Anchor tools are untested on Linux. See the [limits](README.md#limits).
