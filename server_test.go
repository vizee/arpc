package arpc

import (
	"context"
	"reflect"
	"testing"
)

type TestRpcService struct {
}

type TestAddArgs struct {
	A int
	B int
}

type TestAddReply struct {
	C int
}

func (*TestRpcService) Add(_ context.Context, args *TestAddArgs) (*TestAddReply, error) {
	return &TestAddReply{
		C: args.A + args.B,
	}, nil
}

func (*TestRpcService) NotRpc(context.Context, *string) (*string, error) {
	panic(`unreachable`)
}

func (*TestRpcService) invisible(context.Context, *TestAddArgs) (*TestAddReply, error) {
	panic(`unreachable`)
}

func Test_collectRpcMethods(t *testing.T) {
	svc := &TestRpcService{}
	sty := reflect.TypeOf(svc)
	methods := collectRpcMethods(sty)
	t.Log("number of methods:", len(methods))
	for name := range methods {
		t.Log("rpc method:", name)
	}
}
