# dokku-gplive

## What does it do

This plugin reads
  * docker-options
  * urls
  * lists all databases
  * lists all lets encrypt ssl certificates
  
It outputs all these as a single text stream. 

The collector is a single Go binary (`cmd/gplive`), downloaded from the GitHub release matching `plugin.toml` at install time (or built if `go` is present). Stats run every minute from `/etc/cron.d/gplive` as root. Container disk usage (volumes + writable layer) is measured by a low-priority background `gplive disk-refresh` and cached for 10 minutes.

Release: bump `plugin.toml`, then push a matching `vX.Y.Z` tag; the workflow builds amd64/arm64 binaries.

(please note, this is for an internal project and probably won't be much use to anyone)

## Install

```
sudo dokku plugin:install https://github.com/crisward/dokku-gplive.git gplive
```

## Development container

Open this repository in VS Code and choose **Dev Containers: Reopen in Container**.
The container includes Go and the Claude Code CLI and VS Code extension. Run `claude`
in the container to sign in. Claude Code configuration, authentication, and session
history are kept in a Docker volume across container rebuilds.

The persistent volume contains Claude Code credentials, so only use it with trusted
repositories.