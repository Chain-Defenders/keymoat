# Pipelines and editors

KeyMoat asks three kinds of question: whether a project may reach new hosts, whether to go ahead with a signing command, and for a vault password. Without a terminal there is no one to answer. This page says what happens then.

## What changes without a terminal

| Question | With no terminal |
|---|---|
| New hosts in a project's files | Not asked. The hosts are not allowed, and nothing is recorded, so the question comes up the next time you run a command in a terminal. |
| A signing command | Not run. It exits with code 1, unless `KEYMOAT_ASSUME_YES=1` is set. |
| A vault password | Cannot be asked. A command that needs a Solana key from the vault fails. |

Everything else is the same: the sandbox, hidden keys, the network list.

## In a pipeline

A build machine usually holds deploy keys and registry tokens as secrets, and runs `npm install` on every push. That is the same exposure as a developer's machine.

### Build and test jobs

Install KeyMoat and bubblewrap, then run the build through it:

```yaml
- run: sudo apt-get install -y bubblewrap
- run: |
    curl -fLo keymoat https://github.com/Chain-Defenders/keymoat/releases/latest/download/keymoat-linux-amd64
    chmod +x keymoat && sudo mv keymoat /usr/local/bin/
- run: keymoat install
- run: echo "$HOME/.keymoat/bin" >> "$GITHUB_PATH"
- run: npm ci && forge test
```

If bubblewrap cannot start on the runner (Ubuntu restricts unprivileged user namespaces on some images), lift the restriction first:

```yaml
- run: sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0
```

Allow the hosts your project needs ahead of time, since no one will be asked:

```yaml
- run: keymoat allow --global eth-mainnet.g.alchemy.com
```

A secret passed to the job as an environment variable is hidden from builds and tests if it is a wallet key. Other secrets (API tokens) are not wallet keys and stay visible; do not pass them to jobs that do not need them.

### Deploy jobs

```yaml
- run: forge script script/Deploy.s.sol --rpc-url "$RPC_URL" --broadcast
  env:
    KEYMOAT_ASSUME_YES: "1"
    PRIVATE_KEY: ${{ secrets.DEPLOYER_KEY }}
```

`KEYMOAT_ASSUME_YES=1` lets the signing command through. The confirmation is still printed, so the job's log records the chain and the key source.

Set it on the deploy step only. Set for a whole job, it would let any signing command through, including one a dependency starts.

### Guarding against keys in the repository

```yaml
- run: keymoat scan
```

exits with code 1 when a wallet key is in plain text in the project.

## In an editor

An editor runs your tools for you: a test explorer, a language server, a task. Whether those go through KeyMoat depends on the `PATH` the editor was started with.

- **Started from a terminal** (`code .`): the editor has your shell's `PATH`, so its `forge` and `cargo` are KeyMoat's.
- **Started from the dock or a launcher:** on macOS the editor may not have read your shell's start-up file. Check from the editor's own terminal with `keymoat doctor`. An integrated terminal always reads the start-up file; a background language server may not.

When a tool does go through KeyMoat from an editor, there is no terminal: a test run works, new hosts are not allowed until you have answered the question once in a terminal, and a signing command does not run.

Remember what an editor *extension* is: code inside the editor, outside the sandbox. `keymoat extensions` lists the web3 extensions you have that are worth a second look, and [Keys](keys.md) is the defence.

## In your own scripts

A shell script or a `Makefile` that calls `forge`, `npm` or `anchor` goes through KeyMoat when those names resolve to KeyMoat's links, which they do in any shell that read your start-up file. Each tool call is then sandboxed on its own.

To sandbox the script as a whole, including anything else it runs:

```
keymoat run -- ./scripts/build-all.sh
keymoat run -- make test
```

Inside that run, tools are not sandboxed a second time; the outer limits apply to all of them.

## Switches for automation

| Variable | Effect |
|---|---|
| `KEYMOAT_ASSUME_YES=1` | A signing command runs without asking. |
| `KEYMOAT_KEYS=1` | The project's keys are visible to this command. |
| `KEYMOAT_NET=open` | No network limit for this command. |
| `KEYMOAT=off` | This command runs without KeyMoat. |
| `KEYMOAT_HOME=DIR` | Keep settings, log and vault in DIR instead of `~/.keymoat`. |

[Settings](settings.md) has the full list.
