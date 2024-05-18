package providers

import (
	"fmt"
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

func (t UrlTemplate) GenerateTile(authContext *AuthContext, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	if t.Template == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.url template.url", "")
	}

	b, err := tileRequest.GetBounds()

	if err != nil {
		return nil, err
	}

	//width, height (in pixels), srs (in PROJ.4 format), xmin, ymin, xmax, ymax (in projected map units), and zoom
	url := strings.ReplaceAll(t.Template, "$xmin", fmt.Sprintf("%f", b.MinLong))
	url = strings.ReplaceAll(url, "$xmax", fmt.Sprintf("%f", b.MaxLong))
	url = strings.ReplaceAll(url, "$ymin", fmt.Sprintf("%f", b.MinLat))
	url = strings.ReplaceAll(url, "$ymax", fmt.Sprintf("%f", b.MaxLat))
	url = strings.ReplaceAll(url, "$zoom", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "$width", "256") //TODO: allow these being dynamic
	url = strings.ReplaceAll(url, "$height", "256")
	url = strings.ReplaceAll(url, "$srs", "4326") //TODO: decide if I want this to be dynamic

	return getTile(clientConfig, url, make(map[string]string))
}
