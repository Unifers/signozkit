// Package redis instruments a go-redis v9 client, so every command becomes a
// span under the request or job that issued it.
package redis

import (
	"github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
)

// Instrument installs command tracing on rdb, in place:
//
//	if err := signozredis.Instrument(rdb); err != nil {
//		return err
//	}
//
// It takes the client rather than returning one, because go-redis instruments
// through a hook. Commands are traced only when issued with a context holding
// the calling span, which every go-redis method takes as its first argument.
//
// Dial and pipeline metrics are not installed: this kit exports logs and traces
// and has no metrics pipeline to send them to.
func Instrument(rdb goredis.UniversalClient, opts ...redisotel.TracingOption) error {
	return redisotel.InstrumentTracing(rdb, opts...)
}
