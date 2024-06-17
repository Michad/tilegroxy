package layers

import (
	"fmt"

	"github.com/Michad/tilegroxy/internal"
)

type LayerGroup struct {
	LayerMap map[string]*Layer
}

func ConstructLayerGroup() LayerGroup {
	return LayerGroup{make(map[string]*Layer)}
}

func (l *LayerGroup) AddLayer(layer *Layer) error {
	l.LayerMap[layer.Id] = layer
	return nil
}

func (l *LayerGroup) RenderTile(tileRequest internal.TileRequest) (*internal.Image, error) {
	layer := l.LayerMap[tileRequest.LayerName]

	if layer == nil {
		return nil, fmt.Errorf("invalid layer %v", tileRequest.LayerName)
	}

	return layer.RenderTile(tileRequest)
}

func (l *LayerGroup) RenderTileNoCache(tileRequest internal.TileRequest) (*internal.Image, error) {
	layer := l.LayerMap[tileRequest.LayerName]

	if layer == nil {
		return nil, fmt.Errorf("invalid layer %v", tileRequest.LayerName)
	}

	return layer.RenderTileNoCache(tileRequest)
}
