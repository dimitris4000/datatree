# Security

## Intended deployment

Data Tree is intended for one user on a trusted local computer. It has no authentication and must remain bound to a loopback IP address. Do not expose it through a public reverse proxy, tunnel or shared host.

The server can browse, read and save files using the permissions of its process. Run it as your normal user and open only files you intend to edit. Browser sessions and document history are held in memory; explicit Save and Save As operations write to disk. Bundled application assets run locally without third-party connections. Schema resolution is offline.

Loopback host checks, same-origin mutation checks, session isolation, bounded document processing and overwrite protections are security boundaries. Changes must preserve their regression coverage.

## Reporting a vulnerability

Do not post exploitable details or sensitive documents in a public issue. Use the repository's **Security → Advisories → Report a vulnerability** option when enabled. If private reporting is unavailable, open a public issue requesting a private reporting channel without disclosing the vulnerability or attaching private data.

Include the affected commit, minimal synthetic reproducer, expected and observed behavior, and impact in the private report. No response-time or supported-release commitment is currently defined; use the latest reviewed revision.
