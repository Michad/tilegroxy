package providers

type UrlTemplate struct {
	Template string
}

func (t UrlTemplate) Preauth(authContext *AuthContext) error {
	return nil
}

func (t UrlTemplate) GenerateTile(authContext AuthContext, z int, x int, y int) (*Image, error) {
	return nil, nil
}
