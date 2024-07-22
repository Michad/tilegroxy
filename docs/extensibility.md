# Extensibility

Is tilegroxy's out of the box capabilities not sufficing for your use-case?  Luckily tilegroxy is designed to be highly extensible so you can add whatever functionality yourself!  If possible, please consider contributing back whatever functionality you add if it has generic usefulness. 

There are two ways to extend tilegroxy. One is to use the various "custom" options to provide interpreted Go code to implement a provider or authentication scheme. The other is to use tilegroxy as a library and create your own executable with whatever tweaks you need.

## "Custom" 

You might have noticed "custom" listed a few times in the [Configuration documentation](./configuration.md). These options allow you to provide your own custom code that is interpreted on the fly to fulfill the specific needs you have.  These custom options must be written in Go and are interpreted using [Yaegi](https://github.com/traefik/yaegi).  Yaegi offers a full featured implementation of the Go specification without the need to precompile.  

Your custom code must live within a single file for each provider/auth.  It can use the entire standard library including potentially dangerous function calls such as `exec` and `unsafe`; be as cautious using custom providers from third parties as you would be executing any other third party software. 

There is a performance cost of using a custom vs a built-in offering. The exact cost depends on the complexity of your code, however it is typically below 10 milliseconds while tile generation as a whole is usually more than an order of magnitude slower. With authentication especially tilegroxy utilizes caching to mitigate this impact.  However it is something you should keep in mid when deciding whether to implement a Custom provider/auth. Due to these being written in Go, it is easy to convert your custom code to a built-in equivalent if you find this overhead becomes a bottleneck.

Custom caches are not currently possible. This is because it's most likely you would need/want to use an external library to talk to whatever cache, which isn't currently possible (limitation of Yaegi).

### Custom Providers

For cases where the built-in providers don't suffice, you can write your own custom providers.

Example custom providers can be found within [examples/providers](./examples/providers/).   

Custom providers must be within the `custom` package and must import the `tilegroxy/tilegroxy` package for mandatory datatypes. There are two mandatory functions:

```go
func preAuth(*pkg.RequestContext, tilegroxy.ProviderContext, map[string]interface{}, tilegroxy.ClientConfig, tilegroxy.ErrorMessages) (tilegroxy.ProviderContext, error)

func generateTile(*pkg.RequestContext, tilegroxy.ProviderContext, tilegroxy.TileRequest, map[string]interface{}, tilegroxy.ClientConfig,tilegroxy.ErrorMessages) (*tilegroxy.Image, error)
```

The `preAuth` function is responsible for authenticating outgoing requests and returning a token or whatever else is needed. It is called when needed by the application when either `expiration` is reached or an `AuthError` is returned by `generateTile`. A given instance of tilegroxy will only call this method once at a time and then shares the result among threads. However, ProviderContext is not shared between instances of tilegroxy. 

The `generateTile` function is the main function which returns an image for a given tile request. You should never trigger a call to `preAuth` yourself from `generateTile` (instead return an `AuthError`) to prevent excessive calls to the upstream provider from multiple tiles.

The following types are available for custom providers:

| Type | Description |
| --- | --- |
| [RequestContext](./internal/request_context.go) | Contains contextual information specific to the incoming request. Can retrieve headers via the Value method and authz information if configured properly. Do note there won't be a request when seed and test commands are run, this context will be a "Background Context" at those times |
| [ProviderContext](./internal/layers/provider.go) | A struct for on the fly, provider-specific information. It is primarily used to facilitate authentication. Includes an Expiration field to inform the application when to re-auth via the preAuth method (this should occur before auth actually expires). Also includes an auth token field, a auth Bypass field (for un-authed usecases), and a map |
| [TileRequest](./internal/tile_request.go) | The parameters from the user indicating the layer being requested as well as the specific tile coordinate |
| [ClientConfig](./internal/config/config.go) | A struct from the configuration which indicates settings such as static headers and timeouts. See `Client` in [Configuration documentation](./docs/configuration.md) for details |
| [ErrorMessages](./internal/config/config.go) | A struct from the configuration which indicates common error messages. See `Error Messages` in [Configuration documentation](./docs/configuration.md) for details |
| [Image](./internal/utility.go) | The imagery for a given tile. Currently type mapped to []byte |
| [AuthError](./internal/layers/provider.go) | An Error type to indicate an upstream provider returned an auth error that should trigger a new call to preAuth |
| [GetTile](./internal/layers/provider.go) | A utility method that performs an HTTP GET request to a given URL. Use this when possible to ensure all standard Client configurations are honored |

### Custom Authentication

A custom authentication works much the same way as a custom provider.  The code you need to supply only has access to the standard library.  The package must be "custom" and you must include the following function:

```
func validate(string) (bool, time.Time, string, []string)
```

The `validate` method will be supplied with a single token.  The function should then return (in order):

* pass (bool): Whether the token is valid and should allow the request to proceed
* expiration (time.Time): When the authentication status of the token expires and the validate method should be called again. `validate` should return pass=false for already expired tokens
* user identifier (string): An identifier for the user being authenticated. By default this is only used for logging.
* allowed layers ([]string): The specific layer IDs to allow access to with this specific token. Return an empty array to allow access to all of them.

The method how tokens are extracted from the request is configurable. The following modes are available and if multiple are specified the first one (given the order indicated) in the request is utilized:

| Order | Key | Value | 
| --- | --- | --- | 
| 1 | header | Header Name (in Header-Case) | 
| 2 | cookie | Cookie Name |
| 3 | query | Query Parameter Key |
| 4 | path | None (set as empty string) |

No custom types or methods are available.

## Using tilegroxy as a library

Tilegroxy exposes the critical classes needed to create your own executable using tilegroxy that has a different CLI interface or that includes your own custom providers, cache, authentication, or secret sources.  Extending tilegroxy in this way is more complex and requires you to implement your own entry points but allows you to bring in third party libraries as needed and allows you to have fully custom caches. 

Tilegroxy uses a registration system to find and construct its main entities.  As long as you supply a struct that implements the XXXRegistration interface you can call the RegisterXXX method on startup to allow the tilegroxy internal to locate the struct.  For example, here is a minimal Provider implemented in this way:

```

type FailConfig struct {
}

type Fail struct {
	FailConfig
}

func init() {
	layer.RegisterProvider(FailRegistration{})
}

type FailRegistration struct {
}

func (s FailRegistration) InitializeConfig() any {
	return FailConfig{}
}

func (s FailRegistration) Name() string {
	return "fail"
}

func (s FailRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *layer.LayerGroup) (layer.Provider, error) {
	config := cfgAny.(FailConfig)
	return &Fail{config}, nil
}

func (t Fail) PreAuth(ctx *pkg.RequestContext, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return providerContext, nil
}

func (t Fail) GenerateTile(ctx *pkg.RequestContext, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	return nil, errors.New("TODO")
}
```

From here, implementing the provider is the same as implementing a Custom provider.  The other entities can be specified in the same way.

See the [pkg package](../pkg) for other structs and methods available for customizing tilegroxy.