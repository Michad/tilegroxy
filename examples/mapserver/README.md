# Mapserver Example

This folder contains an example of how you can utilize MapServer with tilegroxy directly via the CGI provider.  

The requirement for using this setup is that you have MapServer installed on the same system as tilegroxy and you have mapfiles accessible in a consistent directory structure.  Tilegroxy uses MapServer by making calls to the mapserv CGI application for every incoming tile request.

## Structure

`tilegroxy.yml` is the main tilegroxy configuration file

`mapserver.conf` is the main MapServer configuration file (newly made required as of MapServer 8.0+)

`index.html` is a leaflet map that demos the map from mapserver served up through tilegroxy. It includes the styling of mapserver as PNG tiles in one layer and then a transparent vector tile layer above it to add in interactivity without needing any clients-side styling rules. 

`mapfiles` is a directory containing example .map files. 

`data` is a directory containing a shapefile used by the example mapfiles. These files come from the US Census therefore is open data 