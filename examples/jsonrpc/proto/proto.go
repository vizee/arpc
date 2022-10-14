package proto

import (
	"context"
	"net"

	"github.com/vizee/arpc"
	"github.com/vizee/arpc/codec/jsonrpc"
)

type Greeter interface {
	SayHello(ctx context.Context, in *HelloRequest) (*HelloReply, error)
}

type HelloRequest struct {
	Name string `json:"name"`
}

type HelloReply struct {
	Message string `json:"message"`
}

type greeterClient struct {
	ac *arpc.Client[*jsonrpc.ClientConn[net.Conn], jsonrpc.Codec]
}

func (c *greeterClient) SayHello(ctx context.Context, in *HelloRequest) (*HelloReply, error) {
	var reply HelloReply
	err := arpc.Invoke(c.ac, ctx, "Greeter", "SayHello", in, &reply)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

func NewGreeterClient(ac *arpc.Client[*jsonrpc.ClientConn[net.Conn], jsonrpc.Codec]) Greeter {
	return &greeterClient{
		ac: ac,
	}
}
