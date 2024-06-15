function PreAuth(authContext) {
    return {};
}


function GenerateTile(authContext, params, clientConfig, errorMessages, tileRequest) {
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
print(GenerateTile);
GenerateTile2=GenerateTile;
print(GenerateTile2);

P=5;
GenerateTile2