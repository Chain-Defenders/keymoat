# KeyMoat documentation

KeyMoat keeps wallet keys out of reach of the code you build, test and install. These pages explain how to set it up, what it does on each command, and where its protection ends.

## Start here

| Page | Read it when |
|---|---|
| [Getting started](getting-started.md) | You are installing KeyMoat for the first time. Ten minutes, start to finish. |
| [How it works](how-it-works.md) | You want to know what happens when you type `forge test`. |
| [Threat model](threat-model.md) | You are deciding whether to rely on it: what it stops, what it does not. |

## Using it

| Page | Covers |
|---|---|
| [Keys](keys.md) | Finding keys in plain text, moving them into encrypted storage, signing afterwards. |
| [Network](network.md) | Which hosts a command may reach, and how to allow one. |
| [Signing](signing.md) | The question before a deploy or a send, and which commands get keys. |
| [Pipelines and editors](automation.md) | Running without a terminal: CI, scripts, editor test runners. |

## By tool

| Page | Tools |
|---|---|
| [Foundry](tools/foundry.md) | `forge`, `cast` |
| [Hardhat and Node](tools/node.md) | `npm`, `npx`, `pnpm`, `yarn`, `bun`, Hardhat |
| [Solana and Anchor](tools/solana.md) | `anchor`, `solana`, `spl-token`, `cargo` |

## Reference

| Page | Covers |
|---|---|
| [Commands](commands.md) | Every `keymoat` command and its options. |
| [Settings](settings.md) | `config.json`, the environment switches, and the files KeyMoat keeps. |
| [What is protected](protected.md) | The exact folders and files that are unreadable or unwritable, per system. |
| [Platforms](platforms.md) | macOS, Linux and Windows: what each uses and how they differ. |
| [Troubleshooting](troubleshooting.md) | A command fails under KeyMoat: what the message means and what to do. |

The [changelog](../CHANGELOG.md) and the [security policy](../SECURITY.md) are at the top of the repository.
