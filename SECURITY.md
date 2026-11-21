# Security Policy

## Supported Versions

| Version | Supported |
| ------- | --------- |
| 0.1.0   | Yes       |

Only the latest release receives security fixes.

## Reporting a Vulnerability

Please report vulnerabilities privately. You can either:

- Use a private GitHub security advisory for this repository, or
- Open an issue without any sensitive details so we can arrange a private
  follow-up.

**Never paste the following into an issue, public or private:**

- `api_key` values,
- `base_url` credentials, or
- the contents of `~/.kbcli/config.yaml`.

Note that `kbcli config list` masks secrets, so its output is safe to share
when you need to describe configuration in a report.
