package ollamanative

func replaceTopModelBytes(body []byte, clientModel, providerModel string) ([]byte, error) {
	return append([]byte(nil), body...), nil
}
