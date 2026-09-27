package redis

import (
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestInstrumentAddsHooksWithoutAServer(t *testing.T) {
	// Instrumentation is a hook, so it is installed without dialling anything.
	rdb := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	if err := Instrument(rdb); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
}
