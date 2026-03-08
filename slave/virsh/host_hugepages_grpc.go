package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (s *SlaveVirshService) GetHostHugePages(ctx context.Context, req *grpcVirsh.Empty) (*grpcVirsh.HostHugePagesStateResponse, error) {
	state, err := GetHostHugePages()
	if err != nil {
		return nil, err
	}
	return grpcHostHugePagesState(state), nil
}

func (s *SlaveVirshService) SetHostHugePages(ctx context.Context, req *grpcVirsh.SetHostHugePagesRequest) (*grpcVirsh.HostHugePagesStateResponse, error) {
	state, err := SetHostHugePages(HostHugePagesRequest{
		PageSize:  req.PageSize,
		PageCount: int(req.PageCount),
	})
	if err != nil {
		return nil, err
	}
	return grpcHostHugePagesState(state), nil
}

func (s *SlaveVirshService) RemoveHostHugePages(ctx context.Context, req *grpcVirsh.Empty) (*grpcVirsh.HostHugePagesStateResponse, error) {
	state, err := RemoveHostHugePages()
	if err != nil {
		return nil, err
	}
	return grpcHostHugePagesState(state), nil
}

func grpcHostHugePagesState(state *HostHugePagesState) *grpcVirsh.HostHugePagesStateResponse {
	if state == nil {
		return &grpcVirsh.HostHugePagesStateResponse{}
	}
	return &grpcVirsh.HostHugePagesStateResponse{
		Enabled:                 state.Enabled,
		RebootRequired:          state.RebootRequired,
		Message:                 state.Message,
		PageSize:                state.PageSize,
		PageCount:               int32(state.PageCount),
		ActivePageSize:          state.ActivePageSize,
		ActivePageCount:         int32(state.ActivePageCount),
		DefaultHugepagesz:       state.DefaultHugepagesz,
		Hugepagesz:              state.Hugepagesz,
		Hugepages:               state.Hugepages,
		ActiveDefaultHugepagesz: state.ActiveDefaultHugepagesz,
		ActiveHugepagesz:        state.ActiveHugepagesz,
		ActiveHugepages:         state.ActiveHugepages,
	}
}
