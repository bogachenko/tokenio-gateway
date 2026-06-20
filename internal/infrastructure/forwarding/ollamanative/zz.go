package ollamanative

func replaceTopModelBytes(body []byte, clientModel, providerModel string) ([]byte, error) {
	return swapTopField(body, clientModel, providerModel)
}
