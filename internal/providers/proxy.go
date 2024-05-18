package providers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
)

type Proxy struct {
	Url string
}

func (t Proxy) Preauth(authContext *AuthContext) error {
	return nil
}

func (t Proxy) GenerateTile(authContext *AuthContext, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	if t.Url == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.proxy.url", "")
	}

	url := strings.ReplaceAll(t.Url, "{Z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{Y}", strconv.Itoa(tileRequest.Y))
	url = strings.ReplaceAll(url, "{y}", strconv.Itoa(tileRequest.Y))
	url = strings.ReplaceAll(url, "{X}", strconv.Itoa(tileRequest.X))
	url = strings.ReplaceAll(url, "{x}", strconv.Itoa(tileRequest.X))

	return getTile(clientConfig, url, make(map[string]string))
}
