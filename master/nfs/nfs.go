package nfs

import (
	"512SvMan/db"
	"context"
	"fmt"
	"strings"
	"time"

	pbnfs "github.com/Maruqes/512SvMan/api/proto/nfs"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// Default timeout for NFS operations
// Increased to 90s to allow time for NFS export operations which can be slow
const defaultNFSTimeout = 90 * time.Second

// isConnectionClosingError checks if an error is related to the connection being closed
func isConnectionClosingError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection is closing") ||
		strings.Contains(errStr, "client connection is closing") ||
		strings.Contains(errStr, "transport is closing") ||
		strings.Contains(errStr, "code = Canceled")
}

// waitForConnectionReady waits for the connection to be ready with a timeout
func waitForConnectionReady(conn *grpc.ClientConn, timeout time.Duration) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	state := conn.GetState()
	if state == connectivity.Ready {
		return nil
	}

	if state == connectivity.Shutdown {
		return fmt.Errorf("connection is shutdown")
	}

	// Try to connect if idle or in transient failure
	if state == connectivity.Idle || state == connectivity.TransientFailure {
		conn.Connect()
	}

	// Wait for the connection to become ready
	for {
		if conn.WaitForStateChange(ctx, state) {
			newState := conn.GetState()
			if newState == connectivity.Ready {
				return nil
			}
			if newState == connectivity.Shutdown {
				return fmt.Errorf("connection is shutdown")
			}
			state = newState
		} else {
			// Context expired
			return fmt.Errorf("timeout waiting for connection to be ready, current state: %v", conn.GetState())
		}
	}
}

// withRetry executes an operation with retry logic for connection-related errors
func withRetry(conn *grpc.ClientConn, operation func() error) error {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry with exponential backoff
			waitTime := time.Duration(attempt) * 2 * time.Second
			logger.Warnf("Retrying operation (attempt %d/%d) after %v...", attempt+1, maxRetries, waitTime)
			time.Sleep(waitTime)

			// Try to ensure connection is ready before retry
			if err := waitForConnectionReady(conn, 10*time.Second); err != nil {
				logger.Warnf("Connection not ready before retry: %v", err)
			}
		}

		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		// Only retry on connection-related errors
		if !isConnectionClosingError(lastErr) {
			return lastErr
		}

		logger.Warnf("Operation failed with connection error (attempt %d/%d): %v", attempt+1, maxRetries, lastErr)
	}

	return fmt.Errorf("operation failed after %d retries: %w", maxRetries, lastErr)
}

func CreateSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	return withRetry(conn, func() error {
		client := pbnfs.NewNFSServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
		defer cancel()

		res, err := client.CreateSharedFolder(ctx, folderMount)
		if err != nil {
			return err
		}
		logger.Info("Response from CreateSharedFolder: ", res.GetOk(), ", Created folderMount:", folderMount)
		return nil
	})
}

func SyncSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMountList) error {
	return withRetry(conn, func() error {
		client := pbnfs.NewNFSServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
		defer cancel()

		res, err := client.SyncSharedFolder(ctx, folderMount)
		if err != nil {
			return err
		}
		logger.Info("Response from SyncSharedFolder: ", res.GetOk(), ", Synced folderMount:", folderMount)
		return nil
	})
}

func MountSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	return withRetry(conn, func() error {
		client := pbnfs.NewNFSServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
		defer cancel()

		res, err := client.MountFolder(ctx, folderMount)
		if err != nil {
			return err
		}
		logger.Info("Response from MountSharedFolder: ", res.GetOk(), ", Mounted folderMount:", folderMount)
		return nil
	})
}

func UnmountSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	return withRetry(conn, func() error {
		client := pbnfs.NewNFSServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
		defer cancel()

		res, err := client.UnmountFolder(ctx, folderMount)
		if err != nil {
			return err
		}
		logger.Info("Response from UnmountSharedFolder: ", res.GetOk(), ", Unmounted folderMount:", folderMount)
		return nil
	})
}

func RemoveSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	return withRetry(conn, func() error {
		client := pbnfs.NewNFSServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
		defer cancel()

		res, err := client.RemoveSharedFolder(ctx, folderMount)
		if err != nil {
			return err
		}
		logger.Info("Response from RemoveSharedFolder: ", res.GetOk(), ", Removed folderMount:", folderMount)
		return nil
	})
}

func DownloadISO(conn *grpc.ClientConn, ctx context.Context, isoRequest *pbnfs.DownloadIsoRequest) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.DownloadIso(ctx, isoRequest)
	if err != nil {
		return err
	}
	logger.Info("Response from DownloadISO: ", res.GetOk())
	return nil
}

func GetAllSharedFolders() ([]db.NFSShare, error) {
	return db.GetAllNFShares(context.Background())
}

func GetSharedFolderStatus(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) (*pbnfs.SharedFolderStatusResponse, error) {
	client := pbnfs.NewNFSServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
	defer cancel()
	return client.GetSharedFolderStatus(ctx, folderMount)
}

func ListFolderContents(conn *grpc.ClientConn, path string) (*pbnfs.FolderContents, error) {
	client := pbnfs.NewNFSServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
	defer cancel()
	return client.ListFolderContents(ctx, &pbnfs.FolderPath{
		Path: path,
	})
}

func CanFindFileOrDir(conn *grpc.ClientConn, path string) (bool, error) {
	client := pbnfs.NewNFSServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
	defer cancel()
	res, err := client.CanFindFileOrDir(ctx, &pbnfs.FolderPath{
		Path: path,
	})
	if err != nil {
		return false, err
	}
	return res.GetOk(), nil
}

func CheckReadWrite(conn *grpc.ClientConn, path string) error {
	client := pbnfs.NewNFSServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
	defer cancel()
	res, err := client.CheckReadWrite(ctx, &pbnfs.FolderPath{
		Path: path,
	})
	if err != nil {
		return err
	}
	if !res.GetOk() {
		if msg := res.GetMessage(); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("read/write check failed")
	}
	return nil
}

// CheckFileReadable verifies that a file (like a qcow2 disk) can actually be opened and read.
// This is more thorough than CanFindFileOrDir as it catches stale NFS handles.
func CheckFileReadable(conn *grpc.ClientConn, path string) error {
	client := pbnfs.NewNFSServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
	defer cancel()
	res, err := client.CheckFileReadable(ctx, &pbnfs.FolderPath{
		Path: path,
	})
	if err != nil {
		return err
	}
	if !res.GetOk() {
		if msg := res.GetMessage(); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("file readable check failed")
	}
	return nil
}

func Sync(conn *grpc.ClientConn) error {
	client := pbnfs.NewNFSServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultNFSTimeout)
	defer cancel()
	_, err := client.Sync(ctx, &pbnfs.Empty{})
	if err != nil {
		return err
	}
	return nil
}
