package cache

import (
	"io"
	"testing"

	"github.com/guherbozdogan/kit/endpoint"
	"github.com/guherbozdogan/kit/log"
)

func BenchmarkEndpoints(b *testing.B) {
	var (
		ca      = make(closer)
		cb      = make(closer)
		cmap    = map[string]io.Closer{"a": ca, "b": cb}
		factory = func(instance string) (endpoint.Endpoint, io.Closer, error) { return endpoint.Nop, cmap[instance], nil }
		c       = New(factory, log.NewNopLogger())
	)

	b.ReportAllocs()

	c.Update([]string{"a", "b"})

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Endpoints()
		}
	})
}
