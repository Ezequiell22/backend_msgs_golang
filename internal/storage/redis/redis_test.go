package redisstore

import (
    "context"
    "testing"
    "time"

    miniredis "github.com/alicebob/miniredis/v2"
    redis "github.com/redis/go-redis/v9"
)

func TestRedisStoreFlow(t *testing.T){
    mr, err := miniredis.Run()
    if err != nil { t.Fatal(err) }
    defer mr.Close()
    st := NewWithOptions(&redis.Options{Addr: mr.Addr()})

    ctx := context.Background()
    code := "abc"
    ok, err := st.ReserveCode(ctx, code, time.Minute)
    if err != nil || !ok { t.Fatalf("reserve failed") }

    ok, err = st.AttachCipher(ctx, code, "data", time.Minute)
    if err != nil || !ok { t.Fatalf("attach failed") }

    val, ok, err := st.GetAndDelete(ctx, code)
    if err != nil || !ok { t.Fatalf("getdel failed") }
    if val != "data" { t.Fatalf("unexpected val: %s", val) }

    // should be gone
    _, ok, err = st.GetAndDelete(ctx, code)
    if err != nil { t.Fatalf("getdel err: %v", err) }
    if ok { t.Fatalf("expected missing after burn") }

    if err := st.Ping(ctx); err != nil { t.Fatalf("ping failed: %v", err) }
}

