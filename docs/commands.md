# Commands

Every `keymoat` command. Most days you type none of them: your tools go through KeyMoat on their own once it is installed.

| Command | In one line |
|---|---|
| [`install`](#keymoat-install) | Put KeyMoat in front of your tools. |
| [`uninstall`](#keymoat-uninstall) | Take it out again. |
| [`doctor`](#keymoat-doctor) | Check that it works and what is still in plain text. |
| [`run`](#keymoat-run) | Run any command in the sandbox. |
| [`scan`](#keymoat-scan) | Find wallet keys in plain text. |
| [`keys`](#keymoat-keys) | Move keys into encrypted storage, list them, export them. |
| [`allow`](#keymoat-allow) | Let commands connect to a host. |
| [`extensions`](#keymoat-extensions) | List editor extensions worth a second look. |
| [`log`](#keymoat-log) | Show what was blocked, asked and allowed. |
| `version`, `help` | |

## keymoat install

```
keymoat install [--shell] [--tools NAME,NAME,...]
```

Makes a link named after each tool in `~/.keymoat/bin`, pointing at KeyMoat. With that folder first on your `PATH`, typing `forge` runs KeyMoat, which runs the real `forge`.

| Option | Does |
|---|---|
| `--shell` | Adds the `PATH` line to the end of `~/.zshrc`, or `~/.bashrc` and `~/.bash_profile`. Without it, the line is printed for you to add. |
| `--tools a,b` | Links only these names. Use it to add a tool KeyMoat does not cover by default, such as `node` or `make`. A tool added this way runs in the sandbox with no keys. |

By default it links whichever of these are installed: `forge`, `cast`, `anchor`, `solana`, `spl-token`, `cargo`, `npm`, `npx`, `pnpm`, `pnpx`, `yarn`, `bun`, `bunx`. Run it again after installing a new tool.

The links point at the KeyMoat program where it is now. If you move it, run `keymoat install` again.

## keymoat uninstall

```
keymoat uninstall
```

Removes the links and the line it added to your shell's start-up file. Settings, the log and the vault stay in `~/.keymoat`.

## keymoat doctor

```
keymoat doctor
```

Three checks:

- **Sandbox:** runs a real command in the sandbox and makes sure it cannot read a protected file.
- **Tools:** for each installed tool, whether typing its name reaches KeyMoat.
- **Keys in plain text:** the Solana tool's default wallet and the current project's env files.

`keymoat doctor --json` prints the same checks as one JSON object, for editors and scripts: `sandbox` (`ok`, `detail`), `tools` (each with `name`, `through`, `found`, `real`), `pathFirst`, `shimDir`, `project` and `plainKeys` (each with `file`, `line`, `name`, `kind`, `fix`).

Exits with code 1 when something is wrong.

## keymoat run

```
keymoat run [--keys] [--open-net] -- COMMAND [ARGUMENTS...]
```

Runs any command the way KeyMoat runs your tools. For a tool KeyMoat knows, this is the same as typing the tool's name. For anything else (`make`, `node`, a shell script), the command runs in the sandbox with no keys.

| Option | Does |
|---|---|
| `--keys` | The project's env files and key variables are visible to the command. Same as `KEYMOAT_KEYS=1`. |
| `--open-net` | No network limit. Same as `KEYMOAT_NET=open`. |

The exit code is the command's own.

## keymoat scan

```
keymoat scan [--history] [--json] [DIR...]
```

Looks for wallet keys in plain text. With no folder, it scans the current project. It always checks the Solana tool's default wallet.

| Option | Does |
|---|---|
| `DIR...` | Scan these folders instead. |
| `--history` | Also look through shell history. |
| `--json` | Print findings as JSON: `file`, `line`, `name`, `kind`, `fix`. |

Exits with code 1 when it finds a key. It never prints a key. [Keys](keys.md#find-them) explains what it recognises.

## keymoat keys

```
keymoat keys move [FILE]
keymoat keys list
keymoat keys export NAME --to FILE
```

| Sub-command | Does |
|---|---|
| `move` | With no file: moves every private key in the current project's env files into Foundry keystores. With an env file: that file. With a Solana keypair file: moves it into KeyMoat's vault and removes the plain file. Asks for a name and a password (8 characters or more). |
| `list` | Lists Foundry keystores and vault keys, with the address of each vault key. |
| `export` | Writes a vault key back out as a plain Solana keypair file. The vault entry stays. A Foundry keystore is exported with Foundry: `cast wallet decrypt-keystore`. |

`move` and `export` need a terminal, for the password. [Keys](keys.md) walks through each.

## keymoat allow

```
keymoat allow [--global] HOST...
```

Lets sandboxed commands connect to a host: for the current project, or with `--global` for all. A full URL is accepted; its host is used. `*.example.com` covers `example.com` and everything under it.

There is no `deny` command: to take a host off the list, edit `~/.keymoat/config.json` ([Settings](settings.md)).

## keymoat extensions

```
keymoat extensions [--json]
```

Reads the extensions installed in VS Code, VS Code Insiders, VSCodium, Cursor, Windsurf, Antigravity, Trae, Kiro and VS Code Server, and lists the ones worth checking:

- a name very close to a well-known web3 extension, from a different publisher;
- a web3 extension from a publisher KeyMoat does not know;
- a web3 extension installed from a file, not from the store;
- a web3 extension from an unknown publisher installed in the last two weeks.

It is a list of things to check, not a verdict: it makes no claim that an extension is malicious, and a clean result does not prove one is safe.

## keymoat log

```
keymoat log [-n COUNT] [--json]
```

Shows the last entries (20 by default) of `~/.keymoat/events.jsonl`:

| Kind | Meaning |
|---|---|
| `blocked-host` | A sandboxed command tried to reach a host that is not allowed. |
| `gate-confirmed` | You said yes to a signing command. |
| `gate-declined` | A signing command was not run. |
| `keys-visible` | A command ran with `KEYMOAT_KEYS=1` or `--keys`. |
| `bypass` | A command ran with `KEYMOAT=off`. |

Command lines are stored with any key or password removed.

## Exit codes

| Code | Meaning |
|---|---|
| The tool's own | For any command that ran. |
| 1 | A signing command was declined, or a KeyMoat command found a problem (`scan`, `doctor`). |
| 2 | Wrong usage, or a broken settings file. |
| 126 | The sandbox is not available on this system. |
| 127 | The tool is not installed. |
