package providers

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
)

type Proxy struct {
	Url     string
	InvertY bool		//Used for TMS
}

func (t Proxy) PreAuth(authContext *AuthContext) error {
	return nil
}

func (t Proxy) GenerateTile(authContext *AuthContext, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	if t.Url == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.proxy.url", "")
	}

	y := tileRequest.Y
	if t.InvertY {
		y = int(math.Exp2(float64(tileRequest.Z))) - y - 1
	}

	url := strings.ReplaceAll(t.Url, "{Z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{Y}", strconv.Itoa(y))
	url = strings.ReplaceAll(url, "{y}", strconv.Itoa(y))
	url = strings.ReplaceAll(url, "{X}", strconv.Itoa(tileRequest.X))
	url = strings.ReplaceAll(url, "{x}", strconv.Itoa(tileRequest.X))

	return getTile(clientConfig, url, make(map[string]string))
}
