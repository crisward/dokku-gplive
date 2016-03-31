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