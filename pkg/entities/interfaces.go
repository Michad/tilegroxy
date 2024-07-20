package entities

import (
	"net/http"

	"github.com/Michad/tilegroxy/pkg"
)

type Authentication interface {
	CheckAuthentication(req *http.Request, ctx *pkg.RequestContext) bool
}
