function PreAuth(authContext) {
    return {};
}

function httpGet(theUrl)
{
    var xmlHttp = new XMLHttpRequest();
    xmlHttp.open( "GET", theUrl, false ); // false for synchronous request
    xmlHttp.send( null );
    return xmlHttp.responseText;
}

function GenerateTile(authContext, params, clientConfig, errorMessages, tileRequest) {
    console.log(params.url);

    var url = params.url;
    url = url.replace("z", tileRequest.Z);
    url = url.replace("x", tileRequest.X);
    url = url.replace("y", tileRequest.Y);

    return httpGet(url);
}