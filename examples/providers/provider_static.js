function PreAuth(authContext) {
    return {};
}


function GenerateTile(authContext, params, tileRequest) {
    print(params.url);

    var url = params.url;
    url = url.replace("{z}", tileRequest.Z);
    url = url.replace("{x}", tileRequest.X);
    url = url.replace("{y}", tileRequest.Y);

    // return null;
    var result = fetch(url);
    print(result);
    return result;
}

print("Loaded");