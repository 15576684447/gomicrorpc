package memory

import (
	"context"

	"github.com/micro/go-micro/registry"
)

type servicesKey struct{}

func getServices(ctx context.Context) map[string][]*registry.Service {
	//从ctx中获取值，使用方法为 ctx.Value(key).(*value_type)
	s, ok := ctx.Value(servicesKey{}).(map[string][]*registry.Service)
	if !ok {
		return nil
	}
	return s
}

// Services is an option that preloads service data
//将map[string][]*registry.Service参数作为key-value在ctx中传递
func Services(s map[string][]*registry.Service) registry.Option {
	return func(o *registry.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}
		o.Context = context.WithValue(o.Context, servicesKey{}, s)
	}
}
