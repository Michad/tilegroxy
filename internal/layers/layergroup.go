package layers

import (
	"fmt"
	"log/slog"

	"github.com/Michad/tilegroxy/internal"
)

type LayerGroup struct {
	layerMap map[string]*Layer
}

func ConstructLayerGroup() LayerGroup {
	return LayerGroup{make(map[string]*Layer)}
}

func (l *LayerGroup) ListNames() []string {
	names := make([]string, len(l.layerMap))
	i := 0

	for _, l := range l.layerMap {
		names[i] = l.Id
		i += 1
	}

	return names
}

func (l *LayerGroup) Get(layerName string) *Layer {
	return l.layerMap[layerName]
}

func (l *LayerGroup) Contains(layerName string) bool {
	return l.layerMap[layerName] != nil
}

func (l *LayerGroup) AddLayer(layer *Layer) error {
	l.layerMap[layer.Id] = layer
	return nil
}

func (l *LayerGroup) RenderTile(tileRequest internal.TileRequest) (*internal.Image, error) {
	var img *internal.Image
	var err error

	layer := l.layerMap[tileRequest.LayerName]

	img, err = (*layer.Cache).Lookup(tileRequest)

	if img != nil {
		slog.Debug("Cache hit")
		return img, err
	}

	if err != nil {
		slog.Warn(fmt.Sprintf("Cache read error %v\n", err))
	}

	img, err = l.RenderTileNoCache(tileRequest)

	if err != nil {
		return nil, err
	}

	err = (*layer.Cache).Save(tileRequest, img)

	if err != nil {
		slog.Warn(fmt.Sprintf("Cache save error %v\n", err))
	}

	return img, nil
}

func (l *LayerGroup) RenderTileNoCache(tileRequest internal.TileRequest) (*internal.Image, error) {
	layer := l.layerMap[tileRequest.LayerName]

	if layer == nil {
		return nil, fmt.Errorf("invalid layer %v", tileRequest.LayerName)
	}

	return layer.RenderTileNoCache(tileRequest)
}
