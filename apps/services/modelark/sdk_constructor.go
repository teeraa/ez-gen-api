package modelark

import (
	"github.com/volcengine/ark-runtime-go/arkruntime"
	"net/http"
	"sync"
)

var sdkConstructorMu sync.Mutex

// SDK v0.11 creates a hidden control-plane session backed by http.DefaultClient,
// whose transport/proxy it mutates even when WithHTTPClient is supplied. Only
// construction is serialized; inference requests use their own clients/transports.
func newSDKClient(key string, opts ...arkruntime.ConfigOption) *arkruntime.Client {
	sdkConstructorMu.Lock()
	defer sdkConstructorMu.Unlock()
	return arkruntime.NewByteplusClientWithApiKey(key, opts...)
}
func ownedSDKTransport() http.RoundTripper {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return base.Clone()
	}
	return http.DefaultTransport
}
