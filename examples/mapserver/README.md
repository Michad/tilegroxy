# Mapserver Example

This folder contains an example of how you can utilize MapServer with tilegroxy directly via the CGI provider.  

The requirement for using this setup is that you have MapServer installed on the same system as tilegroxy and you have mapfiles accessible in a consistent directory structure.  Tilegroxy uses MapServer by making calls to the mapserv CGI application for every incoming tile request.

## Structure

`tilegroxy.yml` is the main tilegroxy configuration file

`mapserver.conf` is the main MapServer configuration file (newly made required as of MapServer 8.0+)

`mapfiles` is a directory containing example .map files. These files come unmodified from the MapServer tutorial therefore they are licensed under the MapServer license (MIT) and copyright belongs to Open Source Geospatial Foundation and Regents of the University of Minnesota. The full license is included.

`data` is a directory containing a shapefile used by the example mapfiles. These files come unmodified from the MapServer tutorial therefore they are licensed under the MapServer license (MIT) and copyright belongs to Open Source Geospatial Foundation and Regents of the University of Minnesota. The full license is included.