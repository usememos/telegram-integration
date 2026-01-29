package memogram

import (
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	"google.golang.org/grpc"
)

type MemosClient struct {
	InstanceService   v1pb.InstanceServiceClient
	AuthService       v1pb.AuthServiceClient
	UserService       v1pb.UserServiceClient
	MemoService       v1pb.MemoServiceClient
	AttachmentService v1pb.AttachmentServiceClient
}

func NewMemosClient(conn *grpc.ClientConn) *MemosClient {
	return &MemosClient{
		InstanceService:   v1pb.NewInstanceServiceClient(conn),
		AuthService:       v1pb.NewAuthServiceClient(conn),
		UserService:       v1pb.NewUserServiceClient(conn),
		MemoService:       v1pb.NewMemoServiceClient(conn),
		AttachmentService: v1pb.NewAttachmentServiceClient(conn),
	}
}
