package redis

import (
	"context"

	"github.com/redis/go-redis/v9/auth"
)

func subscribeStreamingCredentials(
	ctx context.Context,
	provider auth.StreamingCredentialsProvider,
	listener auth.CredentialsListener,
) (auth.Credentials, auth.UnsubscribeFunc, error) {
	if contextProvider, ok := provider.(auth.ContextStreamingCredentialsProvider); ok {
		return contextProvider.SubscribeWithContext(ctx, listener)
	}
	return provider.Subscribe(listener)
}
