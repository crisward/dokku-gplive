#!/usr/bin/python3
import json
import os
import re

DOKKU_ROOT = "/home/dokku"

def volumeFromOptions(options):
  volumes = []
  lines = options.split("\n")
  for line in lines:
    if line.startswith("-v "):
      volume = {
        "host":re.search('^-v ([^:]+)',line).group(1),
        "container":re.search('[^:]+$',line).group(0)
      }
      volumes.append(volume)
  return volumes

def servicesFromOptions(options):
  services = []
  lines = options.split("\n")
  for line in lines:
    if line.startswith("--link dokku"):
      service = {
        "type":re.search('^--link dokku\.([^\.]+)',line).group(1),
        "name":re.search('([^\.]+):[^:]+$',line).group(1)
      }
      services.append(service)
  return services

def containers(appnames,services):
  stream = os.popen('docker ps --format \'{"container":"{{ .ID }}", "image": "{{ .Image }}", "name":"{{ .Names }}", "ports":"{{ .Ports }}"},\'')
  output = "["+stream.read().strip()[:-1]+"]"
  details = json.loads(output)
  appcontainers = {}
  for line in details:
    # get app details
    if line["image"].startswith("dokku/") and line["name"].endswith(".ambassador") == False:
      name = line["image"].replace("dokku/","").replace(":latest","")
      if name in appcontainers.keys():
        appcontainers[name].processes+=1
      else:
        line["processes"] = 1
        appcontainers[name]=line
    # get service details
    elif line["name"] in services.keys():
      key = line["name"]
      services[key]["status"] = "running"
      services[key]["container"] = line["container"]
      services[key]["version"] = re.search('[^:]+$',line["image"]).group(0)
    elif line["name"].endswith(".ambassador") and line["name"].replace(".ambassador","") in services.keys():
      key = line["name"].replace(".ambassador","")
      services[key]["exposed"] = ",".join( list(set(re.findall("(\d+->\d+)",line["ports"]))) )
      
  apps = []
  for appname in appnames:
    app = { "name":appname }
    if appname in appcontainers.keys():
      app["container"] = appcontainers[appname]["container"]
      app["status"] = "running"
      app["processes"] = appcontainers[appname]["processes"]
    else: 
      app["container"] = ""
      app["status"] = "stopped"
      app["processes"] = 0

    # get domains
    if os.path.exists(DOKKU_ROOT+"/"+appname+"/VHOST"):
      with open(DOKKU_ROOT+"/"+appname+"/VHOST") as f:
        app["domains"] = list( map(str.strip,f.readlines()) )
    # get docker options
    app["docker_options"] = {}
    for stage in ["build","run","deploy"]:
      path = DOKKU_ROOT+"/"+appname+"/DOCKER_OPTIONS_"+stage.upper()
      if os.path.exists(path):
        with open(path) as f:
          app["docker_options"][stage] = f.read()
          if(stage=="deploy"):
            app["volumes"] = volumeFromOptions(app["docker_options"][stage])
            app["services"] = servicesFromOptions(app["docker_options"][stage])

    apps.append(app)
  state = {"apps":apps,"services":list(services.values())}
  return state

def getAppNames():
  stream = os.popen('dokku --quiet apps:list')
  output = stream.read().strip()
  return output.split("\n")

def getServices():
  services = {}
  types = fileList("/var/lib/dokku/services/")
  for type in types:
    serviceNames = fileList("/var/lib/dokku/services/"+type) 
    for name in serviceNames:
      services["dokku."+type+"."+name]={"name":name,"type":type,"state":"stopped","exposed":""}
  return services
  
def fileList(dir):
  stream = os.popen('ls -a '+dir+' | cat')
  output = stream.read().strip().split("\n")
  return [x for x in output if x.startswith(".")==False]

def main():
  appnames = getAppNames()
  services = getServices()

  state = containers(appnames,services)

  with open("/home/dokku/.gitpushcache/state.json", "w") as outfile:
    json.dump(state,outfile,indent=4)


main()