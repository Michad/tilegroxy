package providers

import "github.com/Michad/tilegroxy/pkg"

type UrlTemplate struct {
	Template string
}

func (t UrlTemplate) Preauth(authContext *AuthContext) error {
	return nil
}

func (t UrlTemplate) GenerateTile(authContext *AuthContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
