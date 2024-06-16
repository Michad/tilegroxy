package layers

import "github.com/Michad/tilegroxy/internal"

type LayerGroup struct {
	layerMap map[string]*Layer
}

func ConstructLayerGroup() LayerGroup {
	return LayerGroup{make(map[string]*Layer)}
}

func (l *LayerGroup) AddLayer(layer *Layer) error {

}

func (l *LayerGroup) RenderTile(tileRequest internal.TileRequest) (*internal.Image, error) {

}

func (l *LayerGroup) RenderTileNoCache(tileRequest internal.TileRequest) (*internal.Image, error) {

}
