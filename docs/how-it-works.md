# How it works

KeyMoat sits between you and your tools. When you type `forge test`, the `forge` that runs first is KeyMoat. It decides what kind of command this is, then runs the real `forge` with only what that command needs.

## The two questions

For every command, KeyMoat answers two questions.

**Does this tool run the project's code?** A build, a test or an install runs code you did not write: dependencies, install scripts, build scripts, test helpers. Those tools are run in a sandbox. A tool that only talks to a chain (`cast`, `solana`, `spl-token`) runs no project code, so it runs as it is.

**Does this command sign?** A deploy or a send needs a key. Everything else does not, and gets none.

| | Does not sign | Signs |
|---|---|---|
| **Runs project code** (`forge`, `anchor`, `cargo`, `npm`, `pnpm`, `yarn`, `bun`) | Sandbox, no keys | You are asked first. Sandbox, with the one key the command names |
| **Runs no project code** (`cast`, `solana`, `spl-token`) | Runs as it is | You are asked first. Runs as it is |

## What the sandbox does

A sandboxed command runs with three limits.

**It cannot read secrets.** Keystores, wallet files, SSH keys, cloud and registry credentials, the login keychain, browser profiles, password stores and shell history are unreadable. So is every `.env` file outside the project. [What is protected](protected.md) has the full list.

**It cannot write outside the project.** The project folder, temporary folders and the toolchains' caches are writable. The rest of your home folder is not: no shell start-up file, no git hook, no editor extension folder can be changed.

**It cannot connect where it likes.** The only way out of the machine is a small proxy that KeyMoat starts for the run. The proxy passes a connection on when its host is allowed, and refuses and records it otherwise. Local services, such as a local node, stay reachable. See [Network](network.md).

## What happens to `.env`

The project's own env files get special treatment, because that is where most keys are.

KeyMoat reads each env file and looks at the *values*. A value shaped like a wallet secret (a 64-digit hex key, a seed phrase, a Solana secret key) marks the file as holding a key.

- **In a command that does not sign,** a file holding a key is hidden completely. So the tool does not lose its settings, KeyMoat passes the file's other variables to it through the environment. `RPC_URL` still works; `PRIVATE_KEY` is not there.
- **In a command that signs,** the project's env files are readable, because the command needs them.

Wallet files kept in the project (a Solana keypair, a `.secret` file with a seed phrase) are hidden the same way; [What is protected](protected.md#wallet-files-inside-the-project) has the rules.

Variables in your shell's own environment are treated the same way: one that holds a wallet secret is left out of a command that does not sign.

The keys every local node hands out (the well-known Anvil and Hardhat accounts) are not treated as secrets, so local scripts keep working.

## What happens before signing

A command that signs on a live network stops and shows what is about to happen:

```
keymoat: this command signs with a real key.

  What       Run a script that sends transactions (forge script --broadcast)
  Network    Base (chain 8453) through mainnet.base.org
  Signs with encrypted keystore "deployer"
  Target     script/Deploy.s.sol

Type "yes" to continue:
```

KeyMoat asks the endpoint itself which chain it is, so the line is true even when an RPC variable is mislabelled. A local node is never asked about. [Signing](signing.md) has the details.

## One run, step by step

`forge test` in a project with `PRIVATE_KEY` in `.env`:

1. Your shell finds `~/.keymoat/bin/forge`, which is KeyMoat.
2. KeyMoat finds the real `forge` further along the `PATH`.
3. It works out the project: the repository you are in, or the nearest folder with a project file.
4. It reads the project's env files and finds the key.
5. `forge test` runs project code and does not sign: sandbox, no keys.
6. It starts the proxy and writes the sandbox's rules for this run.
7. It runs the real `forge test` inside the sandbox, with `PRIVATE_KEY` left out and the rest of `.env` passed through.
8. When forge ends, KeyMoat names any connection it blocked, removes the rules and exits with forge's own exit code.

The wrapper adds about a tenth of a second to a command (measured on an M2 Mac: 124 ms on `forge --version`).

## A tool that calls a tool

An `npm` script that runs `forge build` goes through KeyMoat twice. The inner call sees that it is already inside a sandbox and runs the tool directly: the outer run's limits already apply to everything inside it.

## Where it stops

KeyMoat narrows what untrusted code can do; it does not make untrusted code safe. Read the [threat model](threat-model.md) before you rely on it.
