package providers

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/mitchellh/mapstructure"
)

type Provider interface {
	Preauth(authContext *AuthContext) error
	GenerateTile(authContext *AuthContext, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, tileRequest pkg.TileRequest) (*pkg.Image, error)
}

func ConstructProvider(rawConfig map[string]interface{}) (Provider, error) {

	if rawConfig["name"] == "url template" {
		var result UrlTemplate
		err := mapstructure.Decode(rawConfig, &result)
		return result, err
	}
	if rawConfig["name"] == "proxy" {
		var result Proxy
		err := mapstructure.Decode(rawConfig, &result)
		return result, err
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, errors.New("Unsupported provider " + name)
}

type AuthContext struct {
	Expiration time.Time
	Token      string
	Other      map[string]interface{}
}

type AuthError struct {
	arg     int
	message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("%d - %s", e.arg, e.message)
}

type InvalidContentLengthError struct {
	Length uint
}

func (e *InvalidContentLengthError) Error() string {
	return fmt.Sprintf("Invalid content length %v", e.Length)
}

type InvalidContentTypeError struct {
	ContentType string
}

func (e *InvalidContentTypeError) Error() string {
	return fmt.Sprintf("Invalid content type %v", e.ContentType)
}

type RemoteServerError struct {
	StatusCode int
}

func (e *RemoteServerError) Error() string {
	return fmt.Sprintf("Remote server returned status code %v", e.StatusCode)
}

/**
 * Performs a GET operation against a given URL. Implementing providers should call this when possible. It has
 * standard reusable logic around various config options
 */
func getTile(clientConfig *config.ClientConfig, url string, authHeaders map[string]string) (*pkg.Image, error) {
	log.Printf("Calling url %v\n", url)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", clientConfig.UserAgent)

	for h, v := range clientConfig.StaticHeaders {
		req.Header.Set(h, v)
	}

	for h, v := range authHeaders {
		req.Header.Set(h, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	log.Printf("Response status: %v", resp.StatusCode)

	if !slices.Contains(clientConfig.AllowedStatusCodes, resp.StatusCode) {
		return nil, &RemoteServerError{StatusCode: resp.StatusCode}
	}

	if resp.ContentLength == -1 {

	} else {
		if resp.ContentLength > int64(clientConfig.MaxResponseLength) {
			return nil, &InvalidContentLengthError{uint(resp.ContentLength)}
		}
	}

	img, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, &RemoteServerError{StatusCode: resp.StatusCode}
	}

	if len(img) > int(clientConfig.MaxResponseLength) {
		return nil, &InvalidContentLengthError{uint(len(img))}
	}

	return &img, nil
}
