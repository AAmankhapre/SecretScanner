package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/deepfence/SecretScanner/core"
	"github.com/deepfence/SecretScanner/jobs"
	"github.com/deepfence/SecretScanner/scan"
	pb "github.com/deepfence/agent-plugins-grpc/srcgo"
	"google.golang.org/grpc"
)

type gRPCServer struct {
	socket_path string
	plugin_name string
	pb.UnimplementedSecretScannerServer
	pb.UnimplementedAgentPluginServer
	pb.UnimplementedScannersServer
}

func (s *gRPCServer) ReportJobsStatus(context.Context, *pb.Empty) (*pb.JobReports, error) {
	return &pb.JobReports{
		RunningJobs: jobs.GetRunningJobCount(),
	}, nil
}

func (s *gRPCServer) StopScan(c context.Context, req *pb.StopScanRequest) (*pb.StopScanResult, error) {
	core.GetSession().Log.Error("Received StopScanRequest: %v", *req)
	scanID := req.ScanId
	result := &pb.StopScanResult{
		Success:     true,
		Description: "",
	}

	obj, found := jobs.ScanMap.Load(scanID)
	if !found {
		core.GetSession().Log.Error("SecretScanner::Failed to Stop scan, may have already completed successfully or errored out, scan_id: %s", scanID)
		result.Success = false
		result.Description = "SecretScanner::Failed to Stop scan"
		return result, nil
	} else {
		core.GetSession().Log.Error("SecretScanner::Stop request submitted")
		result.Description = "SecretScanner::Stop request submitted"
	}

	scanCtx := obj.(*scan.ScanContext)
	scanCtx.Stopped.Store(true)

	return result, nil
}

func (s *gRPCServer) GetName(context.Context, *pb.Empty) (*pb.Name, error) {
	return &pb.Name{Str: s.plugin_name}, nil
}

func (s *gRPCServer) GetUID(context.Context, *pb.Empty) (*pb.Uid, error) {
	return &pb.Uid{Str: fmt.Sprintf("%s-%s", s.plugin_name, s.socket_path)}, nil
}

func (s *gRPCServer) FindSecretInfo(c context.Context, r *pb.FindRequest) (*pb.FindResult, error) {
	jobs.DispatchScan(r)
	return &pb.FindResult{}, nil
}

func RunServer(socket_path string, plugin_name string) error {

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	lis, err := net.Listen("unix", fmt.Sprintf("%s", socket_path))
	if err != nil {
		return err
	}
	s := grpc.NewServer()

	go func() {
		<-sigs
		s.GracefulStop()
		done <- true
	}()

	jobs.ScanMap = sync.Map{}

	impl := &gRPCServer{socket_path: socket_path, plugin_name: plugin_name}
	pb.RegisterAgentPluginServer(s, impl)
	pb.RegisterSecretScannerServer(s, impl)
	pb.RegisterScannersServer(s, impl)
	core.GetSession().Log.Info("main: server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}

	<-done
	core.GetSession().Log.Info("main: exiting gracefully")
	return nil
}
