# Security policy

keymoat is a security tool. A flaw in it matters more than a flaw in most software, and we want to hear about one before anyone else does.

## What counts

Report it privately if code running in keymoat's sandbox can:

- read a file on the [protected list](docs/protected.md), a wallet key in the project's env files, or a key from the environment, in a command that does not sign;
- connect to a host that is not allowed, or get data out some other way that is not described in the [threat model](docs/threat-model.md);
- write outside the project and the caches, in a way that lets it run outside the sandbox later;
- get a signing command past the confirmation;
- read a key from the vault or a keystore without the password.

Also report anything in keymoat's own handling of keys: the keystore and vault formats, temporary key files, what is written to the log.

Things the threat model already lists as limits (an allowed host used as a way out, writable caches, code outside the sandbox, the open network for Solana deploys) are known. Ideas for narrowing them are welcome as ordinary issues.

## How to report

Use GitHub's private vulnerability reporting on this repository: **Security → Report a vulnerability**. Please do not open a public issue for a flaw of the kinds above.

Include the system and version, `keymoat version`, and the smallest steps that show the problem. Use decoy files and throwaway keys, never real ones.

## What happens next

- We confirm we received it within three working days.
- We tell you whether we can reproduce it, and what we plan to do, within ten.
- We fix it, release, and credit you in the changelog unless you would rather not be named.

Please give us the chance to release a fix before you publish.

## Supported versions

The latest release. keymoat has no long-term support branches.
