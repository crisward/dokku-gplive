# dokku-gplive

## What does it do

This plugin reads
  * docker-options
  * urls
  * lists all databases
  * lists all lets encrypt ssl certificates
  
It outputs all these as a single text stream. 

It should be significantly quicker than running the commands individually as most of the commands are run
in parallel.

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