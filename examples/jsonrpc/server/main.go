package main

import (
	"context"
	"log"
	"net"

	"github.com/rokumoe/arpc"
	"github.com/rokumoe/arpc/examples/jsonrpc/proto"
	"github.com/rokumoe/arpc/jsonrpc"
)

type GreeterService struct {
}

func (*GreeterService) ServiceName() string {
	return "Greeter"
}

func (*GreeterService) SayHello(ctx context.Context, in *proto.HelloRequest) (*proto.HelloReply, error) {
	log.Printf("SayHello: %s", in.Name)

	return &proto.HelloReply{
		Message: "hello " + in.Name,
	}, nil
}

func main() {
	svc := &GreeterService{}
	srv, err := (&arpc.ServerBuilder{}).Register(svc).WithCodec(jsonrpc.GetCodec()).Build()
	if err != nil {
		log.Fatalf("create server: %v", err)
	}
	ln, err := net.Listen("tcp", ":9876")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go func(conn net.Conn) {
			log.Printf("accept connection from %v", conn.RemoteAddr())
			err := srv.ServeConn(jsonrpc.WrapServerConn(conn))
			if err != nil {
				log.Printf("serve conn: %v", err)
			}
		}(conn)
	}
}
