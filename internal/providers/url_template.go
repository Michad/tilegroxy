package providers

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
)

type UrlTemplate struct {
	Template string
}

func (t UrlTemplate) Preauth(authContext *AuthContext) error {
	return nil
}

func (t UrlTemplate) GenerateTile(authContext *AuthContext, clientConfig config.ClientConfig, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	b, err := tileRequest.GetBounds()

	if err != nil {
		return nil, err
	}

	url := strings.ReplaceAll(t.Template, "$xmin", fmt.Sprintf("%f", b.MinLong))
	url = strings.ReplaceAll(url, "$xmax", fmt.Sprintf("%f", b.MaxLong))
	url = strings.ReplaceAll(url, "$ymin", fmt.Sprintf("%f", b.MinLat))
	url = strings.ReplaceAll(url, "$ymax", fmt.Sprintf("%f", b.MaxLat))
	url = strings.ReplaceAll(url, "$zoom", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "$width", "256") //TODO: allow these being dynamic
	url = strings.ReplaceAll(url, "$height", "256")
	url = strings.ReplaceAll(url, "$srs", "4326") //TODO: decide if I want this to be dynamic

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", clientConfig.UserAgent)

	for h, v := range clientConfig.StaticHeaders {
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
