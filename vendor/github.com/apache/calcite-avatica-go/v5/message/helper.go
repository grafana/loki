package message

type MetadataProvider interface {
	GetMetadata() *RpcMetadata
}

func ServerAddressFromMetadata(message MetadataProvider) string {
	serverAddress := "unknown"
	md := message.GetMetadata()
	if md != nil {
		serverAddress = md.ServerAddress
	}
	return serverAddress
}
