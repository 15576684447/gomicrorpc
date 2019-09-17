package main

import (
	"fmt"
	"github.com/micro/go-micro/transport"
	"time"
)

func SockHandler(sock transport.Socket) {
	for {
		var msg transport.Message
		//接收sock数据
		if err := sock.Recv(&msg); err != nil {
			return
		}
		fmt.Printf("Server Recv: %s\n", string(msg.Body))
		if err := sock.Send(&transport.Message{Body:[]byte("pong")}); err != nil {
			return
		}
	}
}

func main()  {
	httpFd := transport.NewTransport()
	l, err :=httpFd.Listen("127.0.0.1:6969")
	if err != nil {
		fmt.Printf("ERR: %s\n", err)
		return
	}
	defer l.Close()
	//server
	go func() {
		for {
			err := l.Accept(SockHandler)
			if err != nil {
				fmt.Printf("Accept error: %v", err)
				time.Sleep(time.Second)
				continue
			}
		}
	}()
	//client
/*
	wait := make(chan bool)
	go func(tr transport.Transport) {
		cli, err := tr.Dial("127.0.0.1:6969")
		if err != nil {
			fmt.Printf("Dial err: %s\n", err)
		}
		defer cli.Close()
		for i := 0; i < 3; i++ {
			time.Sleep(time.Millisecond * 500)
			if err := cli.Send(&transport.Message{
				Body: []byte(`ping`),
			}); err != nil {
				return
			}
			var m transport.Message
			if err := cli.Recv(&m); err != nil {
				return
			}
			fmt.Printf("Client Recv: %s\n", string(m.Body))
		}
		wait <- true
	}(httpFd)
	//此处是为了等待发送线程结束，不然还没来得及发出，主线程就结束了
	<- wait
 */

	cli, err := httpFd.Dial("127.0.0.1:6969")
	if err != nil {
		fmt.Printf("Dial err: %s\n", err)
	}
	defer cli.Close()
	for i := 0; i < 3; i++ {
		time.Sleep(time.Millisecond * 500)
		if err := cli.Send(&transport.Message{
			Body: []byte(`ping`),
		}); err != nil {
			return
		}
		var m transport.Message
		if err := cli.Recv(&m); err != nil {
			return
		}
		fmt.Printf("Client Recv: %s\n", string(m.Body))
	}
}