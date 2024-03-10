package server

import (
	flag "github.com/spf13/pflag"
	"google.golang.org/grpc"
	"k8s.io/klog"
	"net"
	"os"
	"path/filepath"
)

const (
	SocketsSharedDirectory = "/var/run/aaq-sockets"
	SocketName             = "label-sidecar"
)

func Run() {
	var evaluatorConfig string
	flag.StringVar(&evaluatorConfig, "config", "", "Config to configure Evaluator resource allocation for label apps pods")
	flag.Parse()
	klog.Info("Hello from sidecar " + evaluatorConfig)
	socketPath := filepath.Join(SocketsSharedDirectory, SocketName)
	socket, err := net.Listen("unix", socketPath)
	if err != nil {
		klog.Errorf("Failed to initialized socket on path: %s err: %v", socketPath, err.Error())
		klog.Errorf("Check whether given directory exists and socket name is not already taken by other file")
		os.Exit(1)
	}
	defer os.Remove(socketPath)
	s := Server{NewLabelCalculator(evaluatorConfig)}
	grpcServer := grpc.NewServer()
	RegisterPodUsageServer(grpcServer, &s)

	if err := grpcServer.Serve(socket); err != nil {
		klog.Fatalf("Failed to serve gRPC server over port 9000: %v", err)
	}
}
