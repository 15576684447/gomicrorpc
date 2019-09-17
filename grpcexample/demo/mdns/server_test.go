package mdnsdemo

import (
	"github.com/micro/mdns"
	"sync/atomic"
	"testing"
	"time"
)

func TestServer_StartStop(t *testing.T) {
	s := makeService(t)
	serv, err := mdns.NewServer(&mdns.Config{Zone: s})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer serv.Shutdown()
}

//建立一个mdns服务器，并使用Query函数搜索对应名称的服务，并校验返回的服务与预期的是否相同
func TestServer_Lookup(t *testing.T) {
	//建立mdns服务器
	serv, err := mdns.NewServer(&mdns.Config{Zone: makeServiceWithServiceName(t, "_foobar._tcp")})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer serv.Shutdown()

	entries := make(chan *mdns.ServiceEntry, 1)
	var found int32 = 0
	go func() {
		select {
		case e := <-entries:
			t.Logf("%+v\n", e)
			if e.Name != "hostname._foobar._tcp.local." {
				t.Fatalf("bad: %v", e)
			}
			if e.Port != 80 {
				t.Fatalf("bad: %v", e)
			}
			if e.Info != "Local web server" {
				t.Fatalf("bad: %v", e)
			}
			atomic.StoreInt32(&found, 1)

		case <-time.After(80 * time.Millisecond):
			t.Fatalf("timeout")
		}
	}()
	//配置Query参数
	params := &mdns.QueryParam{
		Service: "_foobar._tcp",
		Domain:  "local",
		Timeout: 50 * time.Millisecond,
		Entries: entries,
	}
	//搜索对应服务
	err = mdns.Query(params)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if atomic.LoadInt32(&found) == 0 {
		t.Fatalf("record not found")
	}
}
