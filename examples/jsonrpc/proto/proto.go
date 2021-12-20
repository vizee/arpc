package proto

import (
	"context"

	"github.com/rokumoe/arpc"
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
	inner *arpc.Client
}

func (c *greeterClient) SayHello(ctx context.Context, in *HelloRequest) (*HelloReply, error) {
	var reply HelloReply
	err := c.inner.Invoke(ctx, "Greeter", "SayHello", in, &reply)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}

func NewGreeterClient(cc *arpc.Client) Greeter {
	return &greeterClient{
		inner: cc,
	}
}
