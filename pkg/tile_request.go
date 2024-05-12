package pkg

type TileRequest struct {
	LayerName string
	Z         int
	X         int
	Y         int
}

func (t TileRequest) GetBounds() (float64, float64, float64, float64) {
	//TODO: implement
	return 0, 0, 0, 0
}
