# Platforms

keymoat uses the operating system's own sandbox. The behaviour is the same on macOS and Linux where the systems allow; this page lists what each uses and where they differ.

## At a glance

| | macOS | Linux | Windows |
|---|---|---|---|
| Supported | Yes | Yes | Inside WSL, as Linux |
| Sandbox | The system sandbox (`sandbox-exec`) | [bubblewrap](https://github.com/containers/bubblewrap) | — |
| Needs installing | Nothing | `bubblewrap` | — |
| Tested with | Foundry, Hardhat, npm, pnpm, yarn, bun, Cargo, Anchor, Solana | Foundry, npm, Cargo | — |

## macOS

keymoat writes a rules file for each run and starts the tool under `/usr/bin/sandbox-exec` with it. The rules start from "nothing is allowed" and add what a build tool needs: reading files (minus the protected ones), writing the project and caches, running programs, and connecting to local addresses.

Things to know:

- **`sandbox-exec` is marked deprecated by Apple.** It has been for years, it still works on current macOS, and other developer tools rely on it. If Apple removes it, keymoat will need a new mechanism on macOS.
- **Every `.env` file on the machine is hidden** by a name rule, except the current project's.
- **Its own temporary folder.** The command's `TMPDIR` is `~/Library/Caches/keymoat/tmp`, not the system's, which is shared with every other program. Tools may keep socket files there and in the project.
- **Local services are shared.** A node or validator you started in another terminal is reachable from a sandboxed command, and so is any other local service, including a local proxy if you run one.
- **A sandboxed command may listen on a port,** on any address.
- **The certificate service is allowed, the keychain is not.** Some tools check TLS certificates through the system, which needs the same service that serves the keychain. keymoat allows the service and makes the keychain's folder unreadable, which leaves the login keychain impossible to open from inside the sandbox.
- **Hardware wallets:** USB access is allowed only for a command that asks for one (`--ledger`, `--trezor`). Not yet tested with a device.

## Linux

keymoat starts the tool under `bwrap` with:

- the whole filesystem mounted read-only;
- an empty folder (or empty file) mounted over each protected path;
- the project and the caches mounted writable;
- a fresh `/tmp`;
- a network of its own, with no route out.

The proxy listens on a socket file that is mounted into the sandbox. Inside, keymoat itself runs as a small forwarder that gives the tools a local port (18080) to use as their proxy, then starts the tool.

Differences from macOS:

| | Linux behaviour | What to do |
|---|---|---|
| Other projects' env files | Hidden only for projects in the folders next to the current one. Linux has no way to hide files by a name rule. | Add other project folders to `protect` in [settings](settings.md). |
| A protected file | Reads as empty, and a protected folder lists as empty, rather than giving "permission denied". | Nothing; some tools report "file is empty" where macOS says "not permitted". |
| Local services | A sandboxed command has its own network: a node started **outside** the run is not reachable from inside it. One started inside the same run is. | Start the node in the same command, or use `KEYMOAT_NET=open`. |
| Caches | A cache folder must exist to be mounted writable, so keymoat creates the caches of installed tools before a run. | Nothing. |
| `/tmp` | Fresh for each run; files a command leaves in `/tmp` are gone afterwards. | Write outputs to the project. |

### Requirements

- **bubblewrap** (`bwrap` on the `PATH`).
- **Unprivileged user namespaces.** Most desktop distributions allow them. On the Ubuntu release keymoat was tested on, bubblewrap worked with the default settings. Where Ubuntu's restriction gets in the way (GitHub's Ubuntu runners are reported to need this), run `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`. Inside a container, the container must allow nested namespaces.

`keymoat doctor` tells you if the sandbox cannot start.

### Not yet tested on Linux

The Solana and Anchor tools. Solana publishes no builds for ARM Linux, which is what the test machine ran; x86 Linux has not been tried. The rules for them are the same as on macOS, where they work.

## Windows

There is no native Windows sandbox in keymoat. Use WSL 2, install bubblewrap there, and keep your projects inside the WSL filesystem. Tools you run from PowerShell or `cmd` are not covered.

## Architecture

keymoat is a single Go program with no runtime dependencies. It builds for Intel and ARM on both systems:

```
GOOS=linux GOARCH=amd64 go build -o keymoat .
```
