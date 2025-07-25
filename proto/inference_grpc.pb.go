// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.5.1
// - protoc             v6.31.1
// source: proto/inference.proto

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.64.0 or later.
const _ = grpc.SupportPackageIsVersion9

const (
	Inference_Predict_FullMethodName = "/proto.Inference/Predict"
)

// InferenceClient is the client API for Inference service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
//
// Il servizio che l'Inference Service espone
type InferenceClient interface {
	// RPC per ottenere una predizione da un set di feature
	Predict(ctx context.Context, in *InferenceRequest, opts ...grpc.CallOption) (*InferenceResponse, error)
}

type inferenceClient struct {
	cc grpc.ClientConnInterface
}

func NewInferenceClient(cc grpc.ClientConnInterface) InferenceClient {
	return &inferenceClient{cc}
}

func (c *inferenceClient) Predict(ctx context.Context, in *InferenceRequest, opts ...grpc.CallOption) (*InferenceResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(InferenceResponse)
	err := c.cc.Invoke(ctx, Inference_Predict_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// InferenceServer is the server API for Inference service.
// All implementations must embed UnimplementedInferenceServer
// for forward compatibility.
//
// Il servizio che l'Inference Service espone
type InferenceServer interface {
	// RPC per ottenere una predizione da un set di feature
	Predict(context.Context, *InferenceRequest) (*InferenceResponse, error)
	mustEmbedUnimplementedInferenceServer()
}

// UnimplementedInferenceServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedInferenceServer struct{}

func (UnimplementedInferenceServer) Predict(context.Context, *InferenceRequest) (*InferenceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Predict not implemented")
}
func (UnimplementedInferenceServer) mustEmbedUnimplementedInferenceServer() {}
func (UnimplementedInferenceServer) testEmbeddedByValue()                   {}

// UnsafeInferenceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to InferenceServer will
// result in compilation errors.
type UnsafeInferenceServer interface {
	mustEmbedUnimplementedInferenceServer()
}

func RegisterInferenceServer(s grpc.ServiceRegistrar, srv InferenceServer) {
	// If the following call pancis, it indicates UnimplementedInferenceServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&Inference_ServiceDesc, srv)
}

func _Inference_Predict_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InferenceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(InferenceServer).Predict(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Inference_Predict_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(InferenceServer).Predict(ctx, req.(*InferenceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Inference_ServiceDesc is the grpc.ServiceDesc for Inference service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Inference_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "proto.Inference",
	HandlerType: (*InferenceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Predict",
			Handler:    _Inference_Predict_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/inference.proto",
}
