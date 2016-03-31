# dokku-gitpushlive

This plugin reads
  * docker-options
  * urls
  * lists all databases
  * lists all lets encrypt ssl certificates
  
It outputs all these as a single text stream. 

It should be significantly quicker than running the commands individually as most of the commands are run
in parallel.