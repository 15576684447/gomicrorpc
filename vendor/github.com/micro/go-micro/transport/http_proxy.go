package transport

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
)

const (
	proxyAuthHeader = "Proxy-Authorization"
)

func getURL(addr string) (*url.URL, error) {
	r := &http.Request{
		URL: &url.URL{
			Scheme: "https",
			Host:   addr,
		},
	}
	//获取proxy服务地址，如果环境变量中指定了HTTP_PROXY, HTTPS_PROXY，则从环境变量中获取，返回URL和nil error
	//如果为设定环境变量，或者环境变量为NO_PROXY，返回nil URL和nil error
	//或者如果主机为host，也返回nil URL和nil error
	return http.ProxyFromEnvironment(r)
}

type pbuffer struct {
	net.Conn
	r io.Reader
}

func (p *pbuffer) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

func proxyDial(conn net.Conn, addr string, proxyURL *url.URL) (_ net.Conn, err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	//连接请求
	r := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: addr},
		Header: map[string][]string{"User-Agent": {"micro/latest"}},
	}
	//获取proxy中的username和passwd信息，设置header
	if user := proxyURL.User; user != nil {
		u := user.Username()
		p, _ := user.Password()
		auth := []byte(u + ":" + p)
		basicAuth := base64.StdEncoding.EncodeToString(auth)
		r.Header.Add(proxyAuthHeader, "Basic "+basicAuth)
	}
	//发送请求
	if err := r.Write(conn); err != nil {
		return nil, fmt.Errorf("failed to write the HTTP request: %v", err)
	}
	//读取回复
	br := bufio.NewReader(conn)
	rsp, err := http.ReadResponse(br, r)
	if err != nil {
		return nil, fmt.Errorf("reading server HTTP response: %v", err)
	}
	defer rsp.Body.Close()
	//如果回复失败，解析失败原因并返回
	if rsp.StatusCode != http.StatusOK {
		dump, err := httputil.DumpResponse(rsp, true)
		if err != nil {
			return nil, fmt.Errorf("failed to do connect handshake, status code: %s", rsp.Status)
		}
		return nil, fmt.Errorf("failed to do connect handshake, response: %q", dump)
	}
	//返回请求结果
	return &pbuffer{Conn: conn, r: br}, nil
}

// Creates a new connection
func newConn(dial func(string) (net.Conn, error)) func(string) (net.Conn, error) {
	return func(addr string) (net.Conn, error) {
		// get the proxy url
		//获取proxy的url
		proxyURL, err := getURL(addr)
		if err != nil {
			return nil, err
		}

		// set to addr
		callAddr := addr

		// got proxy
		//如果设定了proxy，则使用proxy，否则使用addr
		if proxyURL != nil {
			callAddr = proxyURL.Host
		}

		// dial the addr
		c, err := dial(callAddr)
		if err != nil {
			return nil, err
		}

		// do proxy connect if we have proxy url
		if proxyURL != nil {
			//使用proxy尝试去请求连接，如果成功，返回连接可用conn
			c, err = proxyDial(c, addr, proxyURL)
		}

		return c, err
	}
}
