# Security policy

## Supported versions

Only the latest release and the current development branch (`contrib`) receive
security fixes. Please check that a problem still exists in one of them before
reporting it.

## Reporting a vulnerability

Please do **not** open a public issue for security problems.

Use GitHub's private vulnerability reporting instead: open the repository's
**Security** tab and choose **Report a vulnerability**. Include the version,
your configuration (without passwords) and steps to reproduce.

zfswatcher is a hobby project. I will try to answer within about a week and to
fix confirmed problems as soon as time allows, but there is no guaranteed
response or fix time. Reporters are credited in the release notes unless they
prefer otherwise.

## Hardening hints

- The web interface listens on `127.0.0.1:8081` by default. Keep it that way and
  publish it, if needed, through a reverse proxy with TLS and your own
  authentication.
- Replace the example web password (`toor`) with a new hash created by
  `zfswatcher -P`.
- Run the daemon as its own unprivileged user. The Debian package does this by
  default (user `zfswatcher`); only switch to `root` if you need `ledctl`.
- Keep the configuration file (which may contain mail credentials) readable only
  by the daemon's user.
