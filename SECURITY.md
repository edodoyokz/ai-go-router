# Security Policy

## Supported Versions

Security fixes are prioritized for the latest state of the `main` branch.
If stable releases are published later, this policy can be expanded with version-specific support windows.

## Reporting a Vulnerability

Please do **not** open a public GitHub issue for suspected security vulnerabilities.

Instead:

- Contact the maintainer privately using a trusted private channel if you already have one
- If no private channel is available yet, open a minimal GitHub issue asking for a private contact method **without disclosing exploit details**

Please include, when possible:

- A clear description of the vulnerability
- Affected component or file path
- Reproduction steps or proof of concept
- Impact assessment
- Suggested mitigation, if known

## Response Goals

Best-effort goals for security reports:

- Acknowledge receipt within a reasonable time
- Reproduce and assess severity
- Prepare a fix or mitigation
- Coordinate disclosure after a patch is available when appropriate

## Security Notes for Users

Because this project can route AI traffic and manage credentials, users should:

- Keep API keys out of committed files
- Prefer environment variables or local-only config files
- Restrict access to admin endpoints
- Avoid exposing the service publicly without authentication and transport protection
- Review proxy, tunnel, OAuth, and sync settings carefully before production use

## Scope

This policy covers responsible disclosure of vulnerabilities in the repository, binaries, and project-maintained deployment guidance.
