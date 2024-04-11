// Code generated by protoc-gen-go. DO NOT EDIT.
// source: staging/libsidecar/evaluator-server-com/evaluate.proto

/*
Package aaq_sidecar_evaluate is a generated protocol buffer package.

It is generated from these files:

	staging/libsidecar/evaluator-server-com/evaluate.proto

It has these top-level messages:

	PodUsageRequest
	PodUsageResponse
	Error
	HealthCheckRequest
	HealthCheckResponse
	ResourceList
	Pod
*/
package aaq_sidecar_evaluate

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// Define messages
type PodUsageRequest struct {
	Pod       *Pod   `protobuf:"bytes,1,opt,name=pod" json:"pod,omitempty"`
	PodsState []*Pod `protobuf:"bytes,2,rep,name=podsState" json:"podsState,omitempty"`
}

func (m *PodUsageRequest) Reset()                    { *m = PodUsageRequest{} }
func (m *PodUsageRequest) String() string            { return proto.CompactTextString(m) }
func (*PodUsageRequest) ProtoMessage()               {}
func (*PodUsageRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *PodUsageRequest) GetPod() *Pod {
	if m != nil {
		return m.Pod
	}
	return nil
}

func (m *PodUsageRequest) GetPodsState() []*Pod {
	if m != nil {
		return m.PodsState
	}
	return nil
}

type PodUsageResponse struct {
	ResourceList *ResourceList `protobuf:"bytes,1,opt,name=resourceList" json:"resourceList,omitempty"`
	Match        bool          `protobuf:"varint,2,opt,name=match" json:"match,omitempty"`
	Error        *Error        `protobuf:"bytes,3,opt,name=error" json:"error,omitempty"`
}

func (m *PodUsageResponse) Reset()                    { *m = PodUsageResponse{} }
func (m *PodUsageResponse) String() string            { return proto.CompactTextString(m) }
func (*PodUsageResponse) ProtoMessage()               {}
func (*PodUsageResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *PodUsageResponse) GetResourceList() *ResourceList {
	if m != nil {
		return m.ResourceList
	}
	return nil
}

func (m *PodUsageResponse) GetMatch() bool {
	if m != nil {
		return m.Match
	}
	return false
}

func (m *PodUsageResponse) GetError() *Error {
	if m != nil {
		return m.Error
	}
	return nil
}

type Error struct {
	Error        bool   `protobuf:"varint,1,opt,name=error" json:"error,omitempty"`
	ErrorMessage string `protobuf:"bytes,2,opt,name=errorMessage" json:"errorMessage,omitempty"`
}

func (m *Error) Reset()                    { *m = Error{} }
func (m *Error) String() string            { return proto.CompactTextString(m) }
func (*Error) ProtoMessage()               {}
func (*Error) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *Error) GetError() bool {
	if m != nil {
		return m.Error
	}
	return false
}

func (m *Error) GetErrorMessage() string {
	if m != nil {
		return m.ErrorMessage
	}
	return ""
}

type HealthCheckRequest struct {
}

func (m *HealthCheckRequest) Reset()                    { *m = HealthCheckRequest{} }
func (m *HealthCheckRequest) String() string            { return proto.CompactTextString(m) }
func (*HealthCheckRequest) ProtoMessage()               {}
func (*HealthCheckRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

type HealthCheckResponse struct {
	Healthy bool `protobuf:"varint,1,opt,name=healthy" json:"healthy,omitempty"`
}

func (m *HealthCheckResponse) Reset()                    { *m = HealthCheckResponse{} }
func (m *HealthCheckResponse) String() string            { return proto.CompactTextString(m) }
func (*HealthCheckResponse) ProtoMessage()               {}
func (*HealthCheckResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{4} }

func (m *HealthCheckResponse) GetHealthy() bool {
	if m != nil {
		return m.Healthy
	}
	return false
}

type ResourceList struct {
	ResourceListJson []byte `protobuf:"bytes,1,opt,name=resourceListJson,proto3" json:"resourceListJson,omitempty"`
}

func (m *ResourceList) Reset()                    { *m = ResourceList{} }
func (m *ResourceList) String() string            { return proto.CompactTextString(m) }
func (*ResourceList) ProtoMessage()               {}
func (*ResourceList) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{5} }

func (m *ResourceList) GetResourceListJson() []byte {
	if m != nil {
		return m.ResourceListJson
	}
	return nil
}

type Pod struct {
	PodJson []byte `protobuf:"bytes,1,opt,name=podJson,proto3" json:"podJson,omitempty"`
}

func (m *Pod) Reset()                    { *m = Pod{} }
func (m *Pod) String() string            { return proto.CompactTextString(m) }
func (*Pod) ProtoMessage()               {}
func (*Pod) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{6} }

func (m *Pod) GetPodJson() []byte {
	if m != nil {
		return m.PodJson
	}
	return nil
}

func init() {
	proto.RegisterType((*PodUsageRequest)(nil), "evaluate.PodUsageRequest")
	proto.RegisterType((*PodUsageResponse)(nil), "evaluate.PodUsageResponse")
	proto.RegisterType((*Error)(nil), "evaluate.Error")
	proto.RegisterType((*HealthCheckRequest)(nil), "evaluate.HealthCheckRequest")
	proto.RegisterType((*HealthCheckResponse)(nil), "evaluate.HealthCheckResponse")
	proto.RegisterType((*ResourceList)(nil), "evaluate.ResourceList")
	proto.RegisterType((*Pod)(nil), "evaluate.Pod")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for PodUsage service

type PodUsageClient interface {
	PodUsageFunc(ctx context.Context, in *PodUsageRequest, opts ...grpc.CallOption) (*PodUsageResponse, error)
	HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error)
}

type podUsageClient struct {
	cc *grpc.ClientConn
}

func NewPodUsageClient(cc *grpc.ClientConn) PodUsageClient {
	return &podUsageClient{cc}
}

func (c *podUsageClient) PodUsageFunc(ctx context.Context, in *PodUsageRequest, opts ...grpc.CallOption) (*PodUsageResponse, error) {
	out := new(PodUsageResponse)
	err := grpc.Invoke(ctx, "/evaluate.PodUsage/PodUsageFunc", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *podUsageClient) HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error) {
	out := new(HealthCheckResponse)
	err := grpc.Invoke(ctx, "/evaluate.PodUsage/HealthCheck", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for PodUsage service

type PodUsageServer interface {
	PodUsageFunc(context.Context, *PodUsageRequest) (*PodUsageResponse, error)
	HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error)
}

func RegisterPodUsageServer(s *grpc.Server, srv PodUsageServer) {
	s.RegisterService(&_PodUsage_serviceDesc, srv)
}

func _PodUsage_PodUsageFunc_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PodUsageRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PodUsageServer).PodUsageFunc(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/evaluate.PodUsage/PodUsageFunc",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PodUsageServer).PodUsageFunc(ctx, req.(*PodUsageRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _PodUsage_HealthCheck_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HealthCheckRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PodUsageServer).HealthCheck(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/evaluate.PodUsage/HealthCheck",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PodUsageServer).HealthCheck(ctx, req.(*HealthCheckRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _PodUsage_serviceDesc = grpc.ServiceDesc{
	ServiceName: "evaluate.PodUsage",
	HandlerType: (*PodUsageServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "PodUsageFunc",
			Handler:    _PodUsage_PodUsageFunc_Handler,
		},
		{
			MethodName: "HealthCheck",
			Handler:    _PodUsage_HealthCheck_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "staging/libsidecar/evaluator-server-com/evaluate.proto",
}

func init() {
	proto.RegisterFile("staging/libsidecar/evaluator-server-com/evaluate.proto", fileDescriptor0)
}

var fileDescriptor0 = []byte{
	// 376 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x74, 0x52, 0xdd, 0x8a, 0xda, 0x40,
	0x14, 0x26, 0x06, 0x5b, 0x3d, 0xa6, 0x28, 0x53, 0x91, 0x54, 0x5a, 0x94, 0x81, 0x82, 0xb4, 0xa8,
	0x60, 0xa1, 0x17, 0xde, 0xb5, 0xc5, 0xb2, 0xc8, 0x2e, 0xc8, 0x2c, 0x7b, 0xb3, 0x37, 0xcb, 0x98,
	0x1c, 0x4c, 0x58, 0xcd, 0xc4, 0x99, 0x89, 0xb0, 0xcf, 0xb0, 0xcf, 0xb0, 0xef, 0xba, 0x64, 0x92,
	0x98, 0x04, 0x77, 0xef, 0xf2, 0xfd, 0xcc, 0x77, 0xf2, 0x9d, 0x19, 0xf8, 0xad, 0x34, 0xdf, 0x85,
	0xd1, 0x6e, 0xbe, 0x0f, 0xb7, 0x2a, 0xf4, 0xd1, 0xe3, 0x72, 0x8e, 0x27, 0xbe, 0x4f, 0xb8, 0x16,
	0x72, 0xaa, 0x50, 0x9e, 0x50, 0x4e, 0x3d, 0x71, 0x28, 0x48, 0x9c, 0xc5, 0x52, 0x68, 0x41, 0x5a,
	0x05, 0xa6, 0x0f, 0xd0, 0xdd, 0x08, 0xff, 0x4e, 0xf1, 0x1d, 0x32, 0x3c, 0x26, 0xa8, 0x34, 0x19,
	0x81, 0x1d, 0x0b, 0xdf, 0xb5, 0xc6, 0xd6, 0xa4, 0xb3, 0xf8, 0x34, 0x3b, 0x1f, 0xdd, 0x08, 0x9f,
	0xa5, 0x0a, 0xf9, 0x09, 0xed, 0x58, 0xf8, 0xea, 0x56, 0x73, 0x8d, 0x6e, 0x63, 0x6c, 0x5f, 0xda,
	0x4a, 0x9d, 0x3e, 0x5b, 0xd0, 0x2b, 0x27, 0xa8, 0x58, 0x44, 0x0a, 0xc9, 0x12, 0x1c, 0x89, 0x4a,
	0x24, 0xd2, 0xc3, 0xeb, 0x50, 0xe9, 0x7c, 0xd6, 0xa0, 0x0c, 0x61, 0x15, 0x95, 0xd5, 0xbc, 0xa4,
	0x0f, 0xcd, 0x03, 0xd7, 0x5e, 0xe0, 0x36, 0xc6, 0xd6, 0xa4, 0xc5, 0x32, 0x40, 0xbe, 0x43, 0x13,
	0xa5, 0x14, 0xd2, 0xb5, 0x4d, 0x54, 0xb7, 0x8c, 0x5a, 0xa5, 0x34, 0xcb, 0x54, 0xfa, 0x07, 0x9a,
	0x06, 0xa7, 0x29, 0x99, 0xdf, 0xca, 0x52, 0x0c, 0x20, 0x14, 0x1c, 0xf3, 0x71, 0x83, 0x2a, 0xfd,
	0x5f, 0x33, 0xa2, 0xcd, 0x6a, 0x1c, 0xed, 0x03, 0xb9, 0x42, 0xbe, 0xd7, 0xc1, 0xbf, 0x00, 0xbd,
	0xc7, 0x7c, 0x69, 0x74, 0x0e, 0x9f, 0x6b, 0x6c, 0x5e, 0xd4, 0x85, 0x8f, 0x81, 0xa1, 0x9f, 0xf2,
	0x41, 0x05, 0xa4, 0x4b, 0x70, 0xaa, 0x25, 0xc9, 0x0f, 0xe8, 0x55, 0x6b, 0xae, 0x95, 0x88, 0xcc,
	0x11, 0x87, 0x5d, 0xf0, 0x74, 0x04, 0xf6, 0x46, 0xf8, 0x69, 0x78, 0x2c, 0xfc, 0x8a, 0xb3, 0x80,
	0x8b, 0x17, 0x0b, 0x5a, 0xc5, 0xd2, 0xc9, 0x0a, 0x9c, 0xe2, 0xfb, 0x7f, 0x12, 0x79, 0xe4, 0x4b,
	0xed, 0xae, 0xaa, 0x57, 0x3f, 0x1c, 0xbe, 0x25, 0xe5, 0x55, 0xd6, 0xd0, 0xa9, 0x34, 0x24, 0x5f,
	0x4b, 0xeb, 0xe5, 0x3a, 0x86, 0xdf, 0xde, 0x51, 0xb3, 0xac, 0xbf, 0x83, 0xfb, 0x3e, 0xe7, 0xc7,
	0x69, 0xfe, 0x64, 0xcf, 0xde, 0xed, 0x07, 0xf3, 0x3c, 0x7f, 0xbd, 0x06, 0x00, 0x00, 0xff, 0xff,
	0xae, 0xd4, 0x04, 0xa6, 0xd8, 0x02, 0x00, 0x00,
}
